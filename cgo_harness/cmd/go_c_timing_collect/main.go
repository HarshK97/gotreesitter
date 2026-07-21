// Command go_c_timing_collect is the narrow, independently installed process
// launcher used by the locked timing board. It deliberately owns no schedule,
// retry, or estimator policy.
package main

/*
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <unistd.h>

extern char **environ;

static int gts_fexecve(int executable_fd, int source_fd, int output_fd,
                       const char *logical_name, const char *lane,
                       const char *fixture) {
  const int inherited_source_fd = 6;
  if (dup2(source_fd, inherited_source_fd) < 0 ||
      fcntl(inherited_source_fd, F_SETFD, 0) < 0 ||
      dup2(output_fd, STDOUT_FILENO) < 0) {
    return errno;
  }
  char source_fd_text[] = "6";
  char *const argv[] = {(char *)logical_name, "--bench", "--lane",
                        (char *)lane, "--fixture", (char *)fixture,
                        "--source-fd", source_fd_text, NULL};
  fexecve(executable_fd, argv, environ);
  return errno;
}
*/
import "C"

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"unsafe"
)

const openFlags = "O_RDONLY|O_CLOEXEC|O_NOFOLLOW"

type identity struct {
	CanonicalPath string `json:"canonical_path"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	Size          uint64 `json:"size"`
	SHA256        string `json:"sha256"`
}

type descriptorReceipt struct {
	FD        int      `json:"fd"`
	OpenFlags string   `json:"open_flags"`
	Identity  identity `json:"identity"`
}

type executionReceipt struct {
	Schema               string            `json:"schema"`
	ExecutionMethod      string            `json:"execution_method"`
	SourceTransport      string            `json:"source_transport"`
	InheritedSourceFD    int               `json:"inherited_source_fd"`
	Argv                 []string          `json:"argv"`
	ArtifactOpened       descriptorReceipt `json:"artifact_opened"`
	ArtifactRetainedPost descriptorReceipt `json:"artifact_retained_post"`
	ArtifactReopenedPost descriptorReceipt `json:"artifact_reopened_post"`
	FixtureOpened        descriptorReceipt `json:"fixture_opened"`
	FixtureRetainedPost  descriptorReceipt `json:"fixture_retained_post"`
	FixtureReopenedPost  descriptorReceipt `json:"fixture_reopened_post"`
	StdoutCompleteEOF    bool              `json:"stdout_complete_eof"`
	StdoutAtomicPacket   bool              `json:"stdout_atomic_packet"`
	StdoutBytes          uint64            `json:"stdout_bytes"`
	StdoutSHA256         string            `json:"stdout_sha256"`
	ExitCode             int               `json:"exit_code"`
	EnvironmentSHA256    string            `json:"environment_sha256"`
}

func main() {
	if len(os.Args) == 7 && os.Args[1] == "--fexecve-child" {
		childExec()
		return
	}
	var artifactPath, artifactHash, fixturePath, fixtureHash, logicalName, lane, fixtureName, receiptPath, samplePath, environmentSHA256 string
	var artifactDevice, artifactInode, artifactSize, fixtureDevice, fixtureInode, fixtureSize uint64
	flag.StringVar(&artifactPath, "artifact", "", "sealed executable path")
	flag.StringVar(&artifactHash, "artifact-sha256", "", "sealed executable SHA-256")
	flag.Uint64Var(&artifactDevice, "artifact-device", 0, "sealed executable device")
	flag.Uint64Var(&artifactInode, "artifact-inode", 0, "sealed executable inode")
	flag.Uint64Var(&artifactSize, "artifact-size", 0, "sealed executable size")
	flag.StringVar(&fixturePath, "fixture", "", "sealed fixture path")
	flag.StringVar(&fixtureHash, "fixture-sha256", "", "sealed fixture SHA-256")
	flag.Uint64Var(&fixtureDevice, "fixture-device", 0, "sealed fixture device")
	flag.Uint64Var(&fixtureInode, "fixture-inode", 0, "sealed fixture inode")
	flag.Uint64Var(&fixtureSize, "fixture-size", 0, "sealed fixture size")
	flag.StringVar(&logicalName, "logical-name", "", "frozen argv[0]")
	flag.StringVar(&lane, "lane", "", "frozen lane")
	flag.StringVar(&fixtureName, "fixture-name", "", "frozen fixture name")
	flag.StringVar(&receiptPath, "receipt", "", "new execution receipt")
	flag.StringVar(&samplePath, "sample", "", "new raw sample frame")
	flag.StringVar(&environmentSHA256, "environment-sha256", "", "SHA-256 of sorted NUL-terminated environment records")
	flag.Parse()
	if flag.NArg() != 0 || artifactPath == "" || fixturePath == "" || logicalName == "" || lane == "" || fixtureName == "" || receiptPath == "" || samplePath == "" {
		fatal(errors.New("incomplete collector invocation"))
	}
	if environmentHash(os.Environ()) != environmentSHA256 {
		fatal(errors.New("collector environment hash mismatch"))
	}
	artifact, artifactOpened, err := openVerified(artifactPath, artifactHash, artifactDevice, artifactInode, artifactSize)
	if err != nil {
		fatal(fmt.Errorf("artifact: %w", err))
	}
	defer artifact.Close()
	fixture, fixtureOpened, err := openVerified(fixturePath, fixtureHash, fixtureDevice, fixtureInode, fixtureSize)
	if err != nil {
		fatal(fmt.Errorf("fixture: %w", err))
	}
	defer fixture.Close()
	packet, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		fatal(err)
	}
	parentPacket, childPacket := os.NewFile(uintptr(packet[0]), "sample-parent"), os.NewFile(uintptr(packet[1]), "sample-child")
	defer parentPacket.Close()
	command := exec.Command("/proc/self/exe", "--fexecve-child", logicalName, lane, fixtureName, "3", "4")
	command.ExtraFiles = []*os.File{artifact, fixture, childPacket}
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fatal(err)
	}
	_ = childPacket.Close()
	frame := make([]byte, 4096)
	count, _, err := syscall.Recvfrom(int(parentPacket.Fd()), frame, 0)
	if err != nil || count == 0 || count == len(frame) {
		fatal(errors.New("sample packet missing or oversized"))
	}
	frame = frame[:count]
	waitErr := command.Wait()
	second := make([]byte, 1)
	secondCount, _, secondErr := syscall.Recvfrom(int(parentPacket.Fd()), second, 0)
	if secondErr != nil || secondCount != 0 {
		fatal(errors.New("sample stdout contained more than one packet"))
	}
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			fatal(waitErr)
		}
		exitCode = exitErr.ExitCode()
	}
	artifactRetained, err := hashOpenDescriptor(artifact, artifactOpened.CanonicalPath)
	if err != nil || artifactRetained != artifactOpened {
		fatal(errors.New("retained artifact descriptor changed"))
	}
	fixtureRetained, err := hashOpenDescriptor(fixture, fixtureOpened.CanonicalPath)
	if err != nil || fixtureRetained != fixtureOpened {
		fatal(errors.New("retained fixture descriptor changed"))
	}
	artifactReopenedFile, artifactReopened, err := openVerified(artifactPath, artifactHash, artifactDevice, artifactInode, artifactSize)
	if err != nil {
		fatal(fmt.Errorf("reopened artifact: %w", err))
	}
	artifactReopenedFD := int(artifactReopenedFile.Fd())
	_ = artifactReopenedFile.Close()
	fixtureReopenedFile, fixtureReopened, err := openVerified(fixturePath, fixtureHash, fixtureDevice, fixtureInode, fixtureSize)
	if err != nil {
		fatal(fmt.Errorf("reopened fixture: %w", err))
	}
	fixtureReopenedFD := int(fixtureReopenedFile.Fd())
	_ = fixtureReopenedFile.Close()
	if err := writeNew(samplePath, frame); err != nil {
		fatal(err)
	}
	receipt := executionReceipt{
		Schema: "gts-collector-position/v3", ExecutionMethod: "fexecve", SourceTransport: "inherited_verified_fd", InheritedSourceFD: 6,
		Argv:           []string{logicalName, "--bench", "--lane", lane, "--fixture", fixtureName, "--source-fd", "6"},
		ArtifactOpened: descriptor(artifact, artifactOpened), ArtifactRetainedPost: descriptor(artifact, artifactRetained), ArtifactReopenedPost: descriptorFD(artifactReopenedFD, artifactReopened),
		FixtureOpened: descriptor(fixture, fixtureOpened), FixtureRetainedPost: descriptor(fixture, fixtureRetained), FixtureReopenedPost: descriptorFD(fixtureReopenedFD, fixtureReopened),
		StdoutCompleteEOF: true, StdoutAtomicPacket: true, StdoutBytes: uint64(len(frame)), StdoutSHA256: hash(frame), ExitCode: exitCode,
		EnvironmentSHA256: environmentSHA256,
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		fatal(err)
	}
	if err := writeNew(receiptPath, append(encoded, '\n')); err != nil {
		fatal(err)
	}
}

func childExec() {
	execFD, err := strconv.Atoi(os.Args[5])
	if err != nil {
		fatal(err)
	}
	sourceFD, err := strconv.Atoi(os.Args[6])
	if err != nil {
		fatal(err)
	}
	logical, lane, fixture := C.CString(os.Args[2]), C.CString(os.Args[3]), C.CString(os.Args[4])
	defer C.free(unsafe.Pointer(logical))
	defer C.free(unsafe.Pointer(lane))
	defer C.free(unsafe.Pointer(fixture))
	errno := C.gts_fexecve(C.int(execFD), C.int(sourceFD), C.int(5), logical, lane, fixture)
	fatal(fmt.Errorf("fexecve: errno %d", int(errno)))
}

func openVerified(path, wantHash string, device, inode, size uint64) (*os.File, identity, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, identity{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	got, err := hashOpenDescriptor(file, canonical(path))
	if err != nil || got.SHA256 != wantHash || got.Device != device || got.Inode != inode || got.Size != size {
		_ = file.Close()
		return nil, identity{}, errors.New("opened descriptor identity mismatch")
	}
	return file, got, nil
}

func hashOpenDescriptor(file *os.File, path string) (identity, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return identity{}, err
	}
	digest := sha256.New()
	count, err := io.Copy(digest, file)
	if err != nil {
		return identity{}, err
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		return identity{}, err
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFREG || count < 0 || uint64(count) != uint64(stat.Size) {
		return identity{}, errors.New("descriptor is not a stable regular file")
	}
	return identity{CanonicalPath: path, Device: uint64(stat.Dev), Inode: stat.Ino, Size: uint64(stat.Size), SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func descriptor(file *os.File, value identity) descriptorReceipt {
	return descriptorFD(int(file.Fd()), value)
}
func descriptorFD(fd int, value identity) descriptorReceipt {
	return descriptorReceipt{FD: fd, OpenFlags: openFlags, Identity: value}
}
func canonical(path string) string {
	absolute, _ := filepath.Abs(path)
	return filepath.Clean(absolute)
}
func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func environmentHash(entries []string) string {
	copyOfEntries := append([]string(nil), entries...)
	sort.Strings(copyOfEntries)
	digest := sha256.New()
	for _, entry := range copyOfEntries {
		_, _ = digest.Write([]byte(entry))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}
func writeNew(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if count, err := file.Write(data); err != nil || count != len(data) {
		_ = file.Close()
		if err != nil {
			return err
		}
		return io.ErrShortWrite
	}
	return file.Close()
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "go_c_timing_collect:", err); os.Exit(1) }
