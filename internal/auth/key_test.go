package auth

import (
	"strings"
	"testing"
)

func TestGenerateAndParseKey(t *testing.T) {
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Token, "emk_"+k.Prefix+"_") {
		t.Fatalf("token %q does not follow emk_<prefix>_<secret>", k.Token)
	}
	prefix, secret, err := ParseToken(k.Token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prefix != k.Prefix {
		t.Errorf("prefix = %q, want %q", prefix, k.Prefix)
	}
	if !VerifySecret(secret, k.SecretHash) {
		t.Error("secret does not match its hash")
	}
	if len(k.SecretHash) != 32 {
		t.Errorf("hash has %d bytes, want 32", len(k.SecretHash))
	}

	other, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if other.Prefix == k.Prefix || other.Token == k.Token {
		t.Error("two generated keys are equal")
	}
	_, otherSecret, _ := ParseToken(other.Token)
	if VerifySecret(otherSecret, k.SecretHash) {
		t.Error("a different secret matched the hash")
	}
}

func TestParseTokenRejectsMalformed(t *testing.T) {
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	_, secret, _ := ParseToken(k.Token)
	cases := map[string]string{
		"empty":          "",
		"no prefix":      "abc",
		"only marker":    "emk_",
		"uppercase":      strings.ToUpper(k.Token),
		"short secret":   k.Token[:len(k.Token)-1],
		"long secret":    k.Token + "a",
		"missing secret": "emk_" + k.Prefix,
		"wrong marker":   "emx_" + k.Prefix + "_" + secret,
		"invalid chars":  "emk_" + k.Prefix + "_" + strings.Repeat("1", len(secret)),
		"extra part":     "emk_" + k.Prefix + "_" + secret[:len(secret)-2] + "_a",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseToken(token); err == nil {
				t.Errorf("ParseToken(%q) succeeded, want error", token)
			}
		})
	}
}
