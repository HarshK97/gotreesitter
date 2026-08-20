#include <dlfcn.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "tree_sitter/api.h"
#include "subtree.h"
#include "tree.h"

enum {
  GTS_EOF_HISTORY_MAX_INPUT = 1024 * 1024,
  GTS_EOF_HISTORY_MAX_NODES = 100000,
  GTS_EOF_HISTORY_MAX_DEPTH = 512,
};

static uint32_t gts_eof_history_capture_count;
static bool gts_eof_history_capture_failed;

static void gts_eof_history_write_node(
  TSNode node,
  const char *field,
  uint32_t depth,
  uint32_t *nodes
) {
  if (depth > GTS_EOF_HISTORY_MAX_DEPTH || ++*nodes > GTS_EOF_HISTORY_MAX_NODES) {
    gts_eof_history_capture_failed = true;
    fputs("<cap>", stdout);
    return;
  }
  fputc('(', stdout);
  if (field && field[0]) {
    fputs(field, stdout);
    fputc(':', stdout);
  }
  fputs(ts_node_type(node), stdout);
  fprintf(stdout, "[%u-%u]", ts_node_start_byte(node), ts_node_end_byte(node));
  if (!ts_node_is_named(node)) fputs("!anon", stdout);
  if (ts_node_is_extra(node)) fputs("!extra", stdout);
  if (ts_node_is_missing(node)) fputs("!missing", stdout);
  if (ts_node_is_error(node)) fputs("!error", stdout);
  if (ts_node_has_error(node)) fputs("!has-error", stdout);

  uint32_t child_count = ts_node_child_count(node);
  for (uint32_t index = 0; index < child_count; index++) {
    fputc(' ', stdout);
    const char *child_field = ts_node_field_name_for_child(node, index);
    gts_eof_history_write_node(ts_node_child(node, index), child_field, depth + 1, nodes);
  }
  fputc(')', stdout);
}

// The diagnostic runtime calls this function after it constructs each accept
// root and before ts_parser__select_tree sees that root. Retaining the subtree
// gives the temporary TSTree sole extra ownership. Deleting the tree releases
// that ownership before the parser resumes.
void gts_eof_history_capture_root(
  const TSLanguage *language,
  Subtree root,
  uint32_t accept_index,
  uint32_t version
) {
  ts_subtree_retain(root);
  TSTree *tree = ts_tree_new(root, language, NULL, 0);
  uint32_t nodes = 0;
  fprintf(
    stdout,
    "GTS_C_EOF_HISTORY accept=%u version=%u precedence=%d error_cost=%u shape=",
    accept_index,
    version,
    ts_subtree_dynamic_precedence(root),
    ts_subtree_error_cost(root)
  );
  gts_eof_history_write_node(ts_tree_root_node(tree), NULL, 0, &nodes);
  fputc('\n', stdout);
  fflush(stdout);
  ts_tree_delete(tree);
  gts_eof_history_capture_count++;
}

static unsigned char *gts_eof_history_read_stdin(uint32_t *length) {
  unsigned char *source = malloc(GTS_EOF_HISTORY_MAX_INPUT + 1);
  if (!source) return NULL;
  size_t size = fread(source, 1, GTS_EOF_HISTORY_MAX_INPUT + 1, stdin);
  if (ferror(stdin) || size > GTS_EOF_HISTORY_MAX_INPUT) {
    free(source);
    return NULL;
  }
  source[size] = 0;
  *length = (uint32_t)size;
  return source;
}

int main(int argc, char **argv) {
  if (argc != 3) {
    fprintf(stderr, "usage: %s <grammar.so> <language-symbol>\n", argv[0]);
    return 2;
  }

  void *grammar = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
  if (!grammar) {
    fprintf(stderr, "dlopen: %s\n", dlerror());
    return 3;
  }
  const TSLanguage *(*language_fn)(void) =
    (const TSLanguage *(*)(void))dlsym(grammar, argv[2]);
  if (!language_fn) {
    fprintf(stderr, "dlsym %s: %s\n", argv[2], dlerror());
    dlclose(grammar);
    return 4;
  }

  uint32_t source_length = 0;
  unsigned char *source = gts_eof_history_read_stdin(&source_length);
  if (!source) {
    fputs("read source: failed or exceeded cap\n", stderr);
    dlclose(grammar);
    return 5;
  }

  TSParser *parser = ts_parser_new();
  if (!parser || !ts_parser_set_language(parser, language_fn())) {
    fputs("set language: failed\n", stderr);
    free(source);
    if (parser) ts_parser_delete(parser);
    dlclose(grammar);
    return 6;
  }
  TSTree *tree = ts_parser_parse_string(parser, NULL, (const char *)source, source_length);
  if (!tree) {
    fputs("parse: no tree\n", stderr);
    free(source);
    ts_parser_delete(parser);
    dlclose(grammar);
    return 7;
  }

  uint32_t published_nodes = 0;
  fputs("GTS_C_EOF_PUBLISHED shape=", stdout);
  gts_eof_history_write_node(ts_tree_root_node(tree), NULL, 0, &published_nodes);
  fputc('\n', stdout);
  fprintf(stdout, "GTS_C_EOF_SUMMARY captures=%u failed=%u\n",
          gts_eof_history_capture_count, gts_eof_history_capture_failed ? 1u : 0u);

  ts_tree_delete(tree);
  ts_parser_delete(parser);
  free(source);
  dlclose(grammar);
  return gts_eof_history_capture_failed ? 8 : 0;
}
