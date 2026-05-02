package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadCodeFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codes.txt")
	data := "# comment\nuser@example.com = 123456\nOTHER@example.com=654321\ninvalid\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := readCodeFromFile(path, "USER@example.com"); got != "123456" {
		t.Fatalf("readCodeFromFile() = %q, want 123456", got)
	}
	if got := readCodeFromFile(path, "missing@example.com"); got != "" {
		t.Fatalf("readCodeFromFile() = %q, want empty", got)
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" alpha, ,beta,gamma ")
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV() = %#v, want %#v", got, want)
	}
	if got := splitCSV("   "); got != nil {
		t.Fatalf("splitCSV(empty) = %#v, want nil", got)
	}
}

func TestSafeName(t *testing.T) {
	got := safeName("user@example.com/path name")
	if got != "user_example_com_path_name" {
		t.Fatalf("safeName() = %q", got)
	}
}
