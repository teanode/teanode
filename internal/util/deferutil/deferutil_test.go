package deferutil

import (
	"errors"
	"testing"
)

func TestRunNoError(t *testing.T) {
	t.Parallel()

	var outerErr error
	Run(func() error {
		return nil
	}, &outerErr)

	if outerErr != nil {
		t.Fatalf("expected no error, got %s", outerErr)
	}
}

func TestRunDeferErrorSetsOuterErr(t *testing.T) {
	t.Parallel()

	var outerErr error
	Run(func() error {
		return errors.New("close failed")
	}, &outerErr)

	if outerErr == nil {
		t.Fatal("expected outerErr to be set")
	}
	if outerErr.Error() != "close failed" {
		t.Fatalf("expected %q, got %q", "close failed", outerErr.Error())
	}
}

func TestRunDeferErrorDoesNotOverwriteExistingErr(t *testing.T) {
	t.Parallel()

	outerErr := errors.New("original error")
	Run(func() error {
		return errors.New("close failed")
	}, &outerErr)

	// The original error should be preserved.
	if outerErr.Error() != "original error" {
		t.Fatalf("expected %q, got %q", "original error", outerErr.Error())
	}
}

func TestRunNilErrPointer(t *testing.T) {
	t.Parallel()

	// Passing nil err pointer should not panic.
	Run(func() error {
		return nil
	}, nil)

	Run(func() error {
		return errors.New("ignored")
	}, nil)
}

func TestRecoverNoPanic(t *testing.T) {
	t.Parallel()

	// Should not panic when there is nothing to recover.
	func() {
		defer Recover()
	}()
}

func TestRecoverFromPanic(t *testing.T) {
	t.Parallel()

	didComplete := false
	func() {
		defer Recover()
		panic("test panic")
	}()
	didComplete = true

	if !didComplete {
		t.Fatal("expected execution to continue after Recover")
	}
}
