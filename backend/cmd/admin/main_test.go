package main

import (
	"strings"
	"testing"
)

func TestReadLine(t *testing.T) {
	for in, want := range map[string]string{
		"Secret@123\n":     "Secret@123",
		"Secret@123\r\n":   "Secret@123",
		"Secret@123":       "Secret@123", // no trailing newline
		"Pass word 1\nx\n": "Pass word 1",
	} {
		got, err := readLine(strings.NewReader(in))
		if err != nil || got != want {
			t.Errorf("readLine(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := readLine(strings.NewReader("\n")); err == nil {
		t.Error("empty password must fail")
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"other"}, {"create-super-admin"}, {"create-super-admin", "--email", "a@b.c"},
		{"create-super-admin", "--email", "a@b.c", "--name", "A", "--password", "x"}} {
		if err := run(args); err == nil {
			t.Errorf("run(%q) should fail before touching the database", args)
		}
	}
}
