//go:build !gts_workcount

package gotreesitter

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	finalizeDeferGuardMarker = "work-count-assembly: finalize-defer guard"
	popPayloadCensusMarker   = "work-count-assembly: payload-census seam"
)

func TestWorkCountProductionAssemblyHasNoDiagnosticScaffolding(t *testing.T) {
	testBinary := buildProductionTestBinary(t)

	nm := runGoTool(t, "nm", testBinary)
	productionWorkCountSymbol := regexp.MustCompile(`(?m)^\s*[0-9a-f]+\s+\S\s+github\.com/odvcencio/gotreesitter\.(?:\(\*[^)]*\)\.)?workCount`)
	if match := productionWorkCountSymbol.Find(nm); match != nil {
		t.Fatalf("untagged binary retains a production work-count symbol: %s", match)
	}

	finalizeLine := sourceMarkerLine(t, "parser.go", finalizeDeferGuardMarker) - 1
	closures := runGoTool(t, "objdump", "-s", `github.com/odvcencio/gotreesitter\.\(\*Parser\)\.parseInternal\.func`, testBinary)
	finalizeAssembly := uniqueAssemblySectionForLine(t, closures, "parser.go", finalizeLine)
	if bytes.Contains(finalizeAssembly, []byte("runtime.deferreturn")) {
		t.Fatalf("untagged finalizeTree retains defer scaffolding:\n%s", finalizeAssembly)
	}
	assertNoDiagnosticAssembly(t, finalizeAssembly)

	popCensusLine := sourceMarkerLine(t, "parser_reduce.go", popPayloadCensusMarker)
	reduceAssembly := runGoTool(t, "objdump", "-s", `github.com/odvcencio/gotreesitter\.\(\*Parser\)\.selectedReduceWindowsFromGSSWithBudget`, testBinary)
	if hasAssemblyForLine(reduceAssembly, "parser_reduce.go", popCensusLine) {
		t.Fatalf("untagged reduction path retains instructions at the payload-census seam:\n%s", reduceAssembly)
	}
	assertNoDiagnosticAssembly(t, reduceAssembly)
}

func assertNoDiagnosticAssembly(t *testing.T, assembly []byte) {
	t.Helper()
	for _, forbidden := range [][]byte{
		[]byte("workCount"),
		[]byte("work_count_hooks.go:"),
	} {
		if bytes.Contains(assembly, forbidden) {
			t.Fatalf("untagged assembly retains diagnostic code %q:\n%s", forbidden, assembly)
		}
	}
}

func sourceMarkerLine(t *testing.T, name, marker string) int {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	lines := bytes.Split(source, []byte("\n"))
	if got := bytes.Count(source, []byte(marker)); got != 1 {
		t.Fatalf("want one %q marker in %s, got %d", marker, name, got)
	}
	for index, line := range lines {
		if bytes.Contains(line, []byte(marker)) {
			return index + 1
		}
	}
	t.Fatalf("marker %q not found in %s", marker, name)
	return 0
}

func uniqueAssemblySectionForLine(t *testing.T, assembly []byte, file string, line int) []byte {
	t.Helper()
	var matches [][]byte
	for _, section := range splitAssemblySections(assembly) {
		if hasAssemblyForLine(section, file, line) {
			matches = append(matches, section)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("want one assembly section for %s:%d, got %d", file, line, len(matches))
	}
	return matches[0]
}

func splitAssemblySections(assembly []byte) [][]byte {
	const header = "TEXT "
	text := string(assembly)
	starts := regexp.MustCompile(`(?m)^TEXT `).FindAllStringIndex(text, -1)
	sections := make([][]byte, 0, len(starts))
	for index, start := range starts {
		end := len(text)
		if index+1 < len(starts) {
			end = starts[index+1][0]
		}
		section := text[start[0]:end]
		if strings.HasPrefix(section, header) {
			sections = append(sections, []byte(section))
		}
	}
	return sections
}

func hasAssemblyForLine(assembly []byte, file string, line int) bool {
	needle := []byte(filepath.Base(file) + ":" + strconv.Itoa(line))
	for _, assemblyLine := range bytes.Split(assembly, []byte("\n")) {
		if bytes.Contains(assemblyLine, needle) {
			return true
		}
	}
	return false
}

func runGoTool(t *testing.T, arguments ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", append([]string{"tool"}, arguments...)...)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("go tool %s: %v", strings.Join(arguments, " "), ctx.Err())
	}
	if err != nil {
		t.Fatalf("go tool %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return output
}

func buildProductionTestBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "gotreesitter.test")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, ".")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("build untagged test binary: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("build untagged test binary: %v\n%s", err, output)
	}
	return binary
}
