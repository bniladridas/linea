package config

import "testing"

func TestEnvBoolAcceptsTrimmedMixedCaseValues(t *testing.T) {
	t.Setenv("LINEA_TEST_BOOL", " True ")
	if !envBool("LINEA_TEST_BOOL", false) {
		t.Fatal("envBool(True) = false, want true")
	}

	t.Setenv("LINEA_TEST_BOOL", " Off ")
	if envBool("LINEA_TEST_BOOL", true) {
		t.Fatal("envBool(Off) = true, want false")
	}
}

func TestEnvBoolUsesFallbackForUnknownValues(t *testing.T) {
	t.Setenv("LINEA_TEST_BOOL", "maybe")
	if !envBool("LINEA_TEST_BOOL", true) {
		t.Fatal("envBool(unknown) = false, want fallback true")
	}
}
