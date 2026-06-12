package database

// camera_update_password_test.go — B-17: verify CameraUpdate.Password is
// wired correctly end-to-end (struct field, json tag, encrypt/decrypt path).
//
// These are pure-unit tests — no DB connection required. The DB-round-trip
// (UpdateCamera persists the encrypted value and GetCamera decrypts it) is
// covered by the integration job (DATABASE_URL set in CI).

import (
	"encoding/json"
	"reflect"
	"testing"

	"ironsight/internal/crypto"
)

// TestCameraUpdate_PasswordFieldExists: the CameraUpdate struct must have a
// Password field so PATCH /api/cameras/{id} can accept a new password without
// the caller needing to delete + re-add the camera (B-17).
func TestCameraUpdate_PasswordFieldExists(t *testing.T) {
	rt := reflect.TypeOf(CameraUpdate{})
	field, ok := rt.FieldByName("Password")
	if !ok {
		t.Fatal("CameraUpdate is missing the Password field (B-17)")
	}
	// Must be a pointer so an absent field leaves the stored password unchanged.
	if field.Type.Kind() != reflect.Ptr {
		t.Errorf("CameraUpdate.Password must be *string, got %v", field.Type)
	}
	// json tag must be "password,omitempty" so the field is only included when
	// the caller explicitly sets it, not on every PATCH.
	jsonTag := field.Tag.Get("json")
	if jsonTag != "password,omitempty" {
		t.Errorf("CameraUpdate.Password json tag = %q, want %q", jsonTag, "password,omitempty")
	}
}

// TestCameraUpdate_PasswordAbsentLeavesOtherFields: when Password is nil
// (absent from JSON), the struct serialises and deserialises cleanly with
// other fields present — nil pointer is preserved, not replaced with a zero
// value that could inadvertently clear an existing camera password.
func TestCameraUpdate_PasswordAbsentLeavesOtherFields(t *testing.T) {
	name := "test-cam"
	body := `{"name":"test-cam"}`

	var u CameraUpdate
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if u.Password != nil {
		t.Errorf("Password should be nil when absent from JSON, got %q", *u.Password)
	}
	if u.Name == nil || *u.Name != name {
		t.Errorf("Name = %v, want %q", u.Name, name)
	}
}

// TestCameraUpdate_PasswordPresentDeserialises: when Password is supplied in
// JSON it must arrive as a non-nil pointer so UpdateCamera can detect it and
// encrypt + store the new value.
func TestCameraUpdate_PasswordPresentDeserialises(t *testing.T) {
	body := `{"password":"s3cr3t"}`
	var u CameraUpdate
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if u.Password == nil {
		t.Fatal("Password should be non-nil when present in JSON")
	}
	if *u.Password != "s3cr3t" {
		t.Errorf("Password = %q, want %q", *u.Password, "s3cr3t")
	}
}

// TestEncryptDecryptRoundTrip: the encrypt/decrypt helpers used by CreateCamera
// and (after B-17) UpdateCamera must round-trip the plaintext correctly.
// Exercises the same code path that UpdateCamera.Password will call.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	plaintext := "hunter2"

	db := &DB{credentialsKey: key}

	encrypted, err := db.encryptCred(plaintext)
	if err != nil {
		t.Fatalf("encryptCred: %v", err)
	}
	if !crypto.IsEncrypted(encrypted) {
		t.Errorf("encryptCred output is not flagged as encrypted: %q", encrypted)
	}
	if encrypted == plaintext {
		t.Error("encryptCred returned plaintext unchanged — encryption did not run")
	}

	decrypted := db.decryptCred(encrypted)
	if decrypted != plaintext {
		t.Errorf("decryptCred = %q, want %q", decrypted, plaintext)
	}
}

// TestEncryptCred_NoKey_Passthrough: when no credentials key is set,
// encryptCred returns the plaintext unchanged (legacy / key-less deployments).
// UpdateCamera must not reject or corrupt the password in this mode.
func TestEncryptCred_NoKey_Passthrough(t *testing.T) {
	db := &DB{} // no credentialsKey
	result, err := db.encryptCred("plainpassword")
	if err != nil {
		t.Fatalf("encryptCred with no key: unexpected error: %v", err)
	}
	if result != "plainpassword" {
		t.Errorf("encryptCred with no key = %q, want plaintext passthrough", result)
	}
}

// TestEncryptCred_AlreadyEncrypted_Idempotent: re-encrypting an already-
// encrypted value must return it unchanged. This protects against the
// PATCH round-trip where the client reads back the camera (password is
// stripped via json:"-") and sends a new payload — if somehow a
// crypt:v1: prefix leaked, double-encryption is prevented.
func TestEncryptCred_AlreadyEncrypted_Idempotent(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	db := &DB{credentialsKey: key}

	first, err := db.encryptCred("secret")
	if err != nil {
		t.Fatalf("first encryptCred: %v", err)
	}
	second, err := db.encryptCred(first) // already encrypted
	if err != nil {
		t.Fatalf("second encryptCred: %v", err)
	}
	if second != first {
		t.Errorf("re-encrypting an already-encrypted value changed it: want %q, got %q", first, second)
	}
}

// TestCamera_PasswordOmittedFromJSON: Camera.Password must be json:"-" so
// the plaintext credential is never returned in any GET/list response.
// This is a security invariant — verify it structurally so a future refactor
// cannot accidentally expose it.
func TestCamera_PasswordOmittedFromJSON(t *testing.T) {
	cam := Camera{Password: "supersecret"}
	b, err := json.Marshal(cam)
	if err != nil {
		t.Fatalf("json.Marshal Camera: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("json.Marshal Camera returned empty")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal Camera: %v", err)
	}
	if _, ok := m["password"]; ok {
		t.Error("Camera JSON response must NOT include the 'password' field (security: write-only)")
	}
}
