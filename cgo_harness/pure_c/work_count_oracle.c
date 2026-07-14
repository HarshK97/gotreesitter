#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "api.h"
#include "work_count.h"

#ifndef TS_LANG_FN
#error "TS_LANG_FN macro is required"
#endif

extern const TSLanguage *TS_LANG_FN(void);

static char *read_source(const char *path, size_t *out_len) {
  FILE *file = fopen(path, "rb");
  if (!file || fseek(file, 0, SEEK_END) != 0) {
    if (file) fclose(file);
    return NULL;
  }
  long end = ftell(file);
  if (end < 0 || fseek(file, 0, SEEK_SET) != 0) {
    fclose(file);
    return NULL;
  }
  char *buf = malloc((size_t)end + 1u);
  if (!buf) {
    fclose(file);
    return NULL;
  }
  size_t n = fread(buf, 1u, (size_t)end, file);
  if (n != (size_t)end || ferror(file)) {
    free(buf);
    fclose(file);
    return NULL;
  }
  fclose(file);
  buf[n] = '\0';
  *out_len = n;
  return buf;
}

static bool tree_complete(TSTree *tree, uint32_t source_len) {
  if (!tree) return false;
  TSNode root = ts_tree_root_node(tree);
  return !ts_node_is_null(root) && ts_node_end_byte(root) == source_len;
}

static bool write_u32(FILE *file, uint32_t value) {
  unsigned char bytes[4] = {
      (unsigned char)value, (unsigned char)(value >> 8),
      (unsigned char)(value >> 16), (unsigned char)(value >> 24)};
  return fwrite(bytes, 1u, sizeof(bytes), file) == sizeof(bytes);
}

static bool write_string(FILE *file, const char *value) {
  uint32_t len = value ? (uint32_t)strlen(value) : 0u;
  return write_u32(file, len) &&
         (len == 0 || fwrite(value, 1u, len, file) == len);
}

static bool write_node_header(FILE *file, TSNode node, const char *field_name) {
  uint32_t child_count = ts_node_child_count(node);
  gts_work_count_add(&gts_work_count_current.selected_nodes, 1);
  if (child_count == 0) {
    gts_work_count_add(&gts_work_count_current.selected_leaf_nodes, 1);
  } else {
    gts_work_count_add(&gts_work_count_current.selected_parent_nodes, 1);
  }

  if (!write_string(file, ts_node_type(node)) ||
      !write_string(file, field_name) ||
      !write_u32(file, ts_node_start_byte(node)) ||
      !write_u32(file, ts_node_end_byte(node))) {
    return false;
  }
  TSPoint start = ts_node_start_point(node);
  TSPoint end = ts_node_end_point(node);
  if (!write_u32(file, start.row) || !write_u32(file, start.column) ||
      !write_u32(file, end.row) || !write_u32(file, end.column)) {
    return false;
  }
  unsigned char flags = 0u;
  if (ts_node_is_named(node)) flags |= 1u << 0;
  if (ts_node_is_extra(node)) flags |= 1u << 1;
  if (ts_node_is_missing(node)) flags |= 1u << 2;
  if (ts_node_is_error(node)) flags |= 1u << 3;
  if (ts_node_has_error(node)) flags |= 1u << 4;
  return fwrite(&flags, 1u, 1u, file) == 1u &&
         write_u32(file, child_count);
}

static bool write_tree(FILE *file, TSNode root) {
  static const unsigned char magic[] = "gts-deep-tree-v1";
  if (fwrite(magic, 1u, sizeof(magic), file) != sizeof(magic) ||
      !write_node_header(file, root, NULL)) {
    return false;
  }
  TSTreeCursor cursor = ts_tree_cursor_new(root);
  for (;;) {
    if (ts_tree_cursor_goto_first_child(&cursor)) {
      if (!write_node_header(file, ts_tree_cursor_current_node(&cursor),
                             ts_tree_cursor_current_field_name(&cursor))) {
        ts_tree_cursor_delete(&cursor);
        return false;
      }
      continue;
    }
    while (!ts_tree_cursor_goto_next_sibling(&cursor)) {
      if (!ts_tree_cursor_goto_parent(&cursor)) {
        ts_tree_cursor_delete(&cursor);
        return !ferror(file);
      }
    }
    if (!write_node_header(file, ts_tree_cursor_current_node(&cursor),
                           ts_tree_cursor_current_field_name(&cursor))) {
      ts_tree_cursor_delete(&cursor);
      return false;
    }
  }
}

static unsigned long long parse_timeout(const char *raw) {
  char *end = NULL;
  unsigned long long value = strtoull(raw, &end, 10);
  if (!raw[0] || (end && *end)) return 0;
  return value;
}

static void print_counts(GTSWorkCount c, uint32_t source_len,
                         uint32_t root_end_byte, bool root_has_error) {
  printf("{\n");
  printf("  \"schema\": \"gts-work-count-c-child/v3\",\n");
  printf("  \"engine\": \"static-c-instrumented-glr\",\n");
  printf("  \"digest_format\": \"gts-deep-tree-v1\",\n");
  printf("  \"source_bytes\": %u,\n", source_len);
  printf("  \"root_end_byte\": %u,\n", root_end_byte);
  printf("  \"root_has_error\": %s,\n", root_has_error ? "true" : "false");
  printf("  \"counters\": {\n");
  printf("    \"contract\": \"gts-work-count/v2\",\n");
  printf("    \"overflow\": %s,\n", c.overflow ? "true" : "false");
#define PRINT_COUNT(field) \
  printf("    \"" #field "\": %llu,\n", (unsigned long long)c.field)
  PRINT_COUNT(shifts);
  PRINT_COUNT(reductions);
  PRINT_COUNT(accept_actions);
  PRINT_COUNT(explicit_recover_actions);
  PRINT_COUNT(reduction_pop_requests);
  PRINT_COUNT(emitted_pop_paths);
  PRINT_COUNT(emitted_pop_payloads);
  PRINT_COUNT(selected_nodes);
  PRINT_COUNT(selected_parent_nodes);
  PRINT_COUNT(selected_leaf_nodes);
  PRINT_COUNT(table_lookups_proxy);
  PRINT_COUNT(action_entries_examined_proxy);
  PRINT_COUNT(lexer_front_door_calls_proxy);
  PRINT_COUNT(stack_version_creations_proxy);
  PRINT_COUNT(merge_attempts_proxy);
  PRINT_COUNT(merge_successes_proxy);
  PRINT_COUNT(graph_link_additions_proxy);
  PRINT_COUNT(leaf_constructions_proxy);
  PRINT_COUNT(parent_constructions_proxy);
  PRINT_COUNT(pending_parent_constructions_proxy);
#undef PRINT_COUNT
  printf("    \"no_tree_parent_constructions_proxy\": %llu\n",
         (unsigned long long)c.no_tree_parent_constructions_proxy);
  printf("  }\n");
  printf("}\n");
}

int main(int argc, char **argv) {
  if (argc != 4) {
    fputs("status=c_protocol_error\n", stderr);
    fprintf(stderr, "usage: %s <source> <digest-dump> <timeout-us>\n", argv[0]);
    return 2;
  }

  size_t source_len = 0;
  char *source = read_source(argv[1], &source_len);
  if (!source || source_len > UINT32_MAX) {
    fputs("status=c_transport_error\n", stderr);
    free(source);
    return 3;
  }
  unsigned long long timeout_us = parse_timeout(argv[3]);
  if (timeout_us == 0) {
    fputs("status=c_protocol_error\n", stderr);
    free(source);
    return 2;
  }

  TSParser *parser = ts_parser_new();
  if (!parser || !ts_parser_set_language(parser, TS_LANG_FN())) {
    fputs("status=c_parser_error\n", stderr);
    if (parser) ts_parser_delete(parser);
    free(source);
    return 4;
  }
  ts_parser_set_timeout_micros(parser, timeout_us);

  gts_work_count_reset();
  TSTree *tree = ts_parser_parse_string(parser, NULL, source, (uint32_t)source_len);
  if (!tree) {
    fputs("status=c_timeout\n", stderr);
    ts_parser_delete(parser);
    free(source);
    return 10;
  }
  if (!tree_complete(tree, (uint32_t)source_len)) {
    fputs("status=c_incomplete\n", stderr);
    ts_tree_delete(tree);
    ts_parser_delete(parser);
    free(source);
    return 11;
  }

  TSNode root = ts_tree_root_node(tree);
  FILE *dump = fopen(argv[2], "wb");
  bool dump_ok = false;
  if (dump) {
    dump_ok = write_tree(dump, root);
    if (fclose(dump) != 0) dump_ok = false;
    dump = NULL;
  }
  if (!dump_ok) {
    fputs("status=c_digest_error\n", stderr);
    ts_tree_delete(tree);
    ts_parser_delete(parser);
    free(source);
    return 5;
  }
  GTSWorkCount counts = gts_work_count_snapshot();
  print_counts(counts, (uint32_t)source_len, ts_node_end_byte(root),
               ts_node_has_error(root));

  ts_tree_delete(tree);
  ts_parser_delete(parser);
  free(source);
  return ferror(stdout) ? 6 : 0;
}
