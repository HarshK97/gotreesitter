#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "api.h"

extern const TSLanguage *tree_sitter_go(void);

typedef struct {
  uint32_t offset;
  TSPoint start;
  TSPoint end;
} edit_site_t;

static uint64_t now_ns(void) {
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (uint64_t)ts.tv_sec * 1000000000ull + (uint64_t)ts.tv_nsec;
}

static void dump_u32(uint32_t value) {
  unsigned char bytes[4] = {
      (unsigned char)value, (unsigned char)(value >> 8),
      (unsigned char)(value >> 16), (unsigned char)(value >> 24)};
  fwrite(bytes, 1u, sizeof(bytes), stdout);
}

static void dump_string(const char *value) {
  uint32_t len = value ? (uint32_t)strlen(value) : 0u;
  dump_u32(len);
  if (len > 0) {
    fwrite(value, 1u, len, stdout);
  }
}

// Deep dump v1 is a deterministic, little-endian preorder stream. Each node
// carries cross-runtime symbol identity through type+named, byte and point spans,
// named/extra/missing/error flags, child count, incoming field name, and child
// order. Hashing this stream lets the static publication transport be admitted
// against the cgo parity binding and gotreesitter before any timing begins.
static void dump_node(TSNode node, const char *field_name) {
  dump_string(ts_node_type(node));
  dump_string(field_name);
  dump_u32(ts_node_start_byte(node));
  dump_u32(ts_node_end_byte(node));
  TSPoint start = ts_node_start_point(node);
  TSPoint end = ts_node_end_point(node);
  dump_u32(start.row);
  dump_u32(start.column);
  dump_u32(end.row);
  dump_u32(end.column);
  unsigned char flags = 0u;
  if (ts_node_is_named(node)) flags |= 1u << 0;
  if (ts_node_is_extra(node)) flags |= 1u << 1;
  if (ts_node_is_missing(node)) flags |= 1u << 2;
  if (ts_node_is_error(node)) flags |= 1u << 3;
  if (ts_node_has_error(node)) flags |= 1u << 4;
  fwrite(&flags, 1u, 1u, stdout);
  uint32_t child_count = ts_node_child_count(node);
  dump_u32(child_count);
  for (uint32_t i = 0; i < child_count; i++) {
    dump_node(ts_node_child(node, i), ts_node_field_name_for_child(node, i));
  }
}

static void dump_tree(TSTree *tree) {
  static const unsigned char magic[] = "gts-deep-tree-v1";
  fwrite(magic, 1u, sizeof(magic), stdout);
  dump_node(ts_tree_root_node(tree), NULL);
}

static char *make_source(int func_count, size_t *out_len) {
  size_t cap = (size_t)func_count * 64u + 64u;
  char *buf = (char *)malloc(cap);
  if (!buf) {
    return NULL;
  }

  size_t n = 0;
  n += (size_t)snprintf(buf + n, cap - n, "package main\n\n");
  for (int i = 0; i < func_count; i++) {
    n += (size_t)snprintf(buf + n, cap - n, "func f%d() int { v := %d; return v }\n", i, i);
  }

  *out_len = n;
  return buf;
}

static char *read_source(const char *path, size_t *out_len) {
  FILE *file = fopen(path, "rb");
  if (!file) {
    return NULL;
  }
  if (fseek(file, 0, SEEK_END) != 0) {
    fclose(file);
    return NULL;
  }
  long end = ftell(file);
  if (end < 0 || fseek(file, 0, SEEK_SET) != 0) {
    fclose(file);
    return NULL;
  }
  char *buf = (char *)malloc((size_t)end + 1u);
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

static int find_substr(const char *src, size_t n, const char *needle) {
  size_t m = strlen(needle);
  if (m == 0 || m > n) {
    return -1;
  }
  for (size_t i = 0; i + m <= n; i++) {
    if (memcmp(src + i, needle, m) == 0) {
      return (int)i;
    }
  }
  return -1;
}

static TSPoint point_at_offset(const char *src, size_t n, size_t off) {
  TSPoint p = {0, 0};
  if (off > n) {
    off = n;
  }
  for (size_t i = 0; i < off; i++) {
    unsigned char c = (unsigned char)src[i];
    if (c == '\n') {
      p.row += 1;
      p.column = 0;
    } else {
      p.column += 1;
    }
  }
  return p;
}

static bool tree_ok(TSTree *tree, uint32_t source_len) {
  if (!tree) {
    return false;
  }
  TSNode root = ts_tree_root_node(tree);
  return !ts_node_is_null(root) && ts_node_end_byte(root) == source_len &&
         !ts_node_has_error(root);
}

static edit_site_t *collect_edit_sites(const char *src, size_t n, size_t *out_count) {
  const char marker[] = "v := ";
  const size_t marker_len = sizeof(marker) - 1u;
  size_t cap = 64;
  size_t count = 0;
  edit_site_t *sites = (edit_site_t *)malloc(cap * sizeof(edit_site_t));
  if (!sites) {
    return NULL;
  }

  size_t i = 0;
  while (i + marker_len < n) {
    int matched = 1;
    for (size_t j = 0; j < marker_len; j++) {
      if (src[i + j] != marker[j]) {
        matched = 0;
        break;
      }
    }
    if (!matched) {
      i++;
      continue;
    }

    size_t off = i + marker_len;
    while (off + 1u < n && src[off + 1u] >= '0' && src[off + 1u] <= '9') {
      off++;
    }
    if (off < n) {
      if (count == cap) {
        size_t new_cap = cap * 2u;
        edit_site_t *grown = (edit_site_t *)realloc(sites, new_cap * sizeof(edit_site_t));
        if (!grown) {
          free(sites);
          return NULL;
        }
        sites = grown;
        cap = new_cap;
      }
      sites[count].offset = (uint32_t)off;
      sites[count].start = point_at_offset(src, n, off);
      sites[count].end = point_at_offset(src, n, off + 1u);
      count++;
    }
    i = off + 1u;
  }

  *out_count = count;
  return sites;
}

static void bench_full(TSParser *parser, const char *src, uint32_t len, int iters) {
  TSTree *warm = ts_parser_parse_string(parser, NULL, src, len);
  if (!tree_ok(warm, len)) {
    fprintf(stderr, "full parse warmup failed\n");
    exit(2);
  }
  ts_tree_delete(warm);

  uint64_t t0 = now_ns();
  for (int i = 0; i < iters; i++) {
    TSTree *tree = ts_parser_parse_string(parser, NULL, src, len);
    if (!tree_ok(tree, len)) {
      fprintf(stderr, "full parse failed\n");
      exit(2);
    }
    ts_tree_delete(tree);
  }
  uint64_t t1 = now_ns();
  printf("pure_c_full_ns_op=%.2f\n", (double)(t1 - t0) / (double)iters);
}

static void bench_inc_edit(TSParser *parser, char *src, uint32_t len, int iters) {
  int edit_at = find_substr(src, len, "v := 0");
  if (edit_at < 0) {
    fprintf(stderr, "edit marker not found\n");
    exit(2);
  }
  edit_at += (int)strlen("v := ");

  TSPoint start = point_at_offset(src, len, (size_t)edit_at);
  TSPoint end = point_at_offset(src, len, (size_t)edit_at + 1u);

  TSTree *tree = ts_parser_parse_string(parser, NULL, src, len);
  if (!tree_ok(tree, len)) {
    fprintf(stderr, "initial parse failed\n");
    exit(2);
  }

  TSInputEdit edit;
  memset(&edit, 0, sizeof(edit));
  edit.start_byte = (uint32_t)edit_at;
  edit.old_end_byte = (uint32_t)edit_at + 1u;
  edit.new_end_byte = (uint32_t)edit_at + 1u;
  edit.start_point = start;
  edit.old_end_point = end;
  edit.new_end_point = end;

  uint64_t t0 = now_ns();
  for (int i = 0; i < iters; i++) {
    src[edit_at] = (src[edit_at] == '0') ? '1' : '0';
    ts_tree_edit(tree, &edit);
    TSTree *new_tree = ts_parser_parse_string(parser, tree, src, len);
    if (!tree_ok(new_tree, len)) {
      fprintf(stderr, "incremental edit parse failed\n");
      exit(2);
    }
    ts_tree_delete(tree);
    tree = new_tree;
  }
  uint64_t t1 = now_ns();
  printf("pure_c_inc_edit_ns_op=%.2f\n", (double)(t1 - t0) / (double)iters);

  ts_tree_delete(tree);
}

static void bench_inc_noedit(TSParser *parser, const char *src, uint32_t len, int iters) {
  TSTree *tree = ts_parser_parse_string(parser, NULL, src, len);
  if (!tree_ok(tree, len)) {
    fprintf(stderr, "initial parse failed\n");
    exit(2);
  }

  uint64_t t0 = now_ns();
  for (int i = 0; i < iters; i++) {
    TSTree *new_tree = ts_parser_parse_string(parser, tree, src, len);
    if (!tree_ok(new_tree, len)) {
      fprintf(stderr, "incremental no-edit parse failed\n");
      exit(2);
    }
    ts_tree_delete(tree);
    tree = new_tree;
  }
  uint64_t t1 = now_ns();
  printf("pure_c_inc_noedit_ns_op=%.2f\n", (double)(t1 - t0) / (double)iters);

  ts_tree_delete(tree);
}

static void bench_inc_random_edit(TSParser *parser, char *src, uint32_t len, int iters) {
  size_t site_count = 0;
  edit_site_t *sites = collect_edit_sites(src, len, &site_count);
  if (!sites || site_count == 0) {
    fprintf(stderr, "random edit sites not found\n");
    free(sites);
    exit(2);
  }

  TSTree *tree = ts_parser_parse_string(parser, NULL, src, len);
  if (!tree_ok(tree, len)) {
    fprintf(stderr, "initial parse failed\n");
    free(sites);
    exit(2);
  }

  uint32_t seed = 0x9e3779b9u;
  uint64_t t0 = now_ns();
  for (int i = 0; i < iters; i++) {
    seed = seed * 1664525u + 1013904223u;
    const edit_site_t site = sites[seed % (uint32_t)site_count];

    const uint32_t off = site.offset;
    src[off] = (src[off] == '0') ? '1' : '0';

    TSInputEdit edit;
    memset(&edit, 0, sizeof(edit));
    edit.start_byte = off;
    edit.old_end_byte = off + 1u;
    edit.new_end_byte = off + 1u;
    edit.start_point = site.start;
    edit.old_end_point = site.end;
    edit.new_end_point = site.end;

    ts_tree_edit(tree, &edit);
    TSTree *new_tree = ts_parser_parse_string(parser, tree, src, len);
    if (!tree_ok(new_tree, len)) {
      fprintf(stderr, "incremental random edit parse failed\n");
      free(sites);
      exit(2);
    }
    ts_tree_delete(tree);
    tree = new_tree;
  }
  uint64_t t1 = now_ns();
  printf("pure_c_inc_edit_random_ns_op=%.2f\n", (double)(t1 - t0) / (double)iters);

  ts_tree_delete(tree);
  free(sites);
}

int main(int argc, char **argv) {
  int func_count = 500;
  int full_iters = 2000;
  int inc_iters = 20000;

  bool dump_mode = argc >= 3 && strcmp(argv[1], "--dump") == 0;
  bool dump_synthetic_mode = argc >= 3 && strcmp(argv[1], "--dump-synthetic") == 0;

  if (dump_synthetic_mode) {
    func_count = atoi(argv[2]);
  } else if (!dump_mode && argc >= 2) {
    func_count = atoi(argv[1]);
  }
  if (!dump_mode && !dump_synthetic_mode && argc >= 3) {
    full_iters = atoi(argv[2]);
  }
  if (!dump_mode && !dump_synthetic_mode && argc >= 4) {
    inc_iters = atoi(argv[3]);
  }

  size_t source_len = 0;
  const char *source_path = dump_mode ? argv[2] : (argc >= 5 ? argv[4] : NULL);
  char *source = source_path ? read_source(source_path, &source_len)
                             : make_source(func_count, &source_len);
  if (!source) {
    fprintf(stderr, "source load failed%s%s\n", source_path ? ": " : "",
            source_path ? source_path : "");
    return 2;
  }

  TSParser *parser = ts_parser_new();
  if (!parser || !ts_parser_set_language(parser, tree_sitter_go())) {
    fprintf(stderr, "parser init failed\n");
    free(source);
    return 2;
  }

  if (dump_mode || dump_synthetic_mode) {
    TSTree *tree = ts_parser_parse_string(parser, NULL, source, (uint32_t)source_len);
    if (!tree_ok(tree, (uint32_t)source_len)) {
      fprintf(stderr, "deep dump parse failed\n");
      ts_parser_delete(parser);
      free(source);
      return 2;
    }
    dump_tree(tree);
    ts_tree_delete(tree);
    ts_parser_delete(parser);
    free(source);
    return ferror(stdout) ? 2 : 0;
  }

  printf("source_bytes=%zu\n", source_len);
  printf("source_kind=%s\n", source_path ? "file" : "synthetic_lr_control");
  bench_full(parser, source, (uint32_t)source_len, full_iters);
  if (!source_path) {
    bench_inc_edit(parser, source, (uint32_t)source_len, inc_iters);
    bench_inc_noedit(parser, source, (uint32_t)source_len, inc_iters);
    bench_inc_random_edit(parser, source, (uint32_t)source_len, inc_iters);
  }

  ts_parser_delete(parser);
  free(source);
  return 0;
}
