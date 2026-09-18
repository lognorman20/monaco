package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func checkStagedEnv(t *testing.T, files ...string) (string, error) {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts", "githooks", "check-staged-env.sh")}, files...)...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCheckStagedEnv_allowsExampleAndEncrypted(t *testing.T) {
	dir := t.TempDir()
	example := filepath.Join(dir, ".env.example")
	if err := os.WriteFile(example, []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enc := filepath.Join(dir, ".env.local")
	if err := os.WriteFile(enc, []byte("#/-------------------[DOTENV_PUBLIC_KEY]--------------------/\nDOTENV_PUBLIC_KEY_LOCAL=\"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := checkStagedEnv(t, example, enc, readme)
	if err != nil {
		t.Fatalf("expected pass, err=%v out=%s", err, out)
	}
}

func TestCheckStagedEnv_blocksPlaintextEnvAndKeys(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, ".env.local")
	if err := os.WriteFile(plain, []byte("SECRET=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keys := filepath.Join(dir, ".env.keys")
	if err := os.WriteFile(keys, []byte("DOTENV_PRIVATE_KEY=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := checkStagedEnv(t, plain, keys)
	if err == nil {
		t.Fatalf("expected fail, out=%s", out)
	}
	if !strings.Contains(out, "plaintext .env") {
		t.Fatalf("missing plaintext error: %s", out)
	}
	if !strings.Contains(out, "do not commit") {
		t.Fatalf("missing keys error: %s", out)
	}
}
