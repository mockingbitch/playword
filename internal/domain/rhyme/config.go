// Package rhyme decides what rhymes: dialect mapping, rhyme keys at five
// strictness levels and ranking. It is pure (no DB, no HTTP); the rules
// themselves live in YAML (configs/), see ADR-001.
package rhyme

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrInvalidConfig is matched by every configuration error.
var ErrInvalidConfig = errors.New("invalid rhyme config")

// FamiliesConfig is configs/rhyme_families.yaml.
type FamiliesConfig struct {
	Version         int                 `yaml:"version"`
	NucleusFamilies map[string][]string `yaml:"nucleus_families"`
	CodaFamilies    map[string][]string `yaml:"coda_families"`
	CrossCoda       [][]string          `yaml:"cross_coda_equivalents"`
	L3CodaMode      string              `yaml:"l3_coda_mode"` // merged (default) | families
}

// DialectsConfig is configs/dialects.yaml.
type DialectsConfig struct {
	Version int          `yaml:"version"`
	Bac     DialectRules `yaml:"bac"`
	Trung   DialectRules `yaml:"trung"`
	Nam     DialectRules `yaml:"nam"`
}

// DialectRules apply in a fixed order (nucleus, coda, tone) and every
// condition looks at the original syllable.
type DialectRules struct {
	NucleusRules []NucleusRule `yaml:"nucleus_rules"`
	CodaRules    []CodaRule    `yaml:"coda_rules"`
	ToneMerge    map[int]int   `yaml:"tone_merge"`
}

// NucleusRule rewrites nucleus From to To when the coda is in WhenCoda.
type NucleusRule struct {
	WhenCoda []string `yaml:"when_coda"`
	From     string   `yaml:"from"`
	To       string   `yaml:"to"`
}

// CodaRule rewrites From to To when the nucleus is in WhenNucleus (empty =
// any) and not in UnlessNucleus.
type CodaRule struct {
	From          string   `yaml:"from"`
	To            string   `yaml:"to"`
	WhenNucleus   []string `yaml:"when_nucleus"`
	UnlessNucleus []string `yaml:"unless_nucleus"`
}

func decodeStrict(r io.Reader, v any) error {
	d := yaml.NewDecoder(r)
	d.KnownFields(true)
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return nil
}

// LoadFamilies decodes rhyme_families.yaml; unknown keys are errors.
func LoadFamilies(r io.Reader) (*FamiliesConfig, error) {
	var c FamiliesConfig
	return &c, decodeStrict(r, &c)
}

// LoadDialects decodes dialects.yaml; unknown keys are errors.
func LoadDialects(r io.Reader) (*DialectsConfig, error) {
	var c DialectsConfig
	return &c, decodeStrict(r, &c)
}

// KeyerVersion is part of ConfigID: bump it whenever a code change (rime
// table, key format, dialect semantics) changes the keys produced from the
// same YAML, so stored keys built by older code are detected as stale.
const KeyerVersion = 3

// LoadDir builds an Engine from rhyme_families.yaml and dialects.yaml in
// dir. Its ConfigID is the SHA-256 of both files, which ties stored rhyme
// keys to the rules that produced them (ADR-003 D1).
func LoadDir(dir string) (*Engine, error) {
	famSrc, fam, err := loadFile(filepath.Join(dir, "rhyme_families.yaml"), LoadFamilies)
	if err != nil {
		return nil, err
	}
	diaSrc, dia, err := loadFile(filepath.Join(dir, "dialects.yaml"), LoadDialects)
	if err != nil {
		return nil, err
	}
	e, err := NewEngine(fam, dia)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	fmt.Fprintf(h, "keyer-v%d:%d:%s%d:%s", KeyerVersion, len(famSrc), famSrc, len(diaSrc), diaSrc)
	e.configID = hex.EncodeToString(h.Sum(nil))
	e.familiesYAML, e.dialectsYAML = string(famSrc), string(diaSrc)
	return e, nil
}

func loadFile[T any](path string, load func(io.Reader) (*T, error)) ([]byte, *T, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	c, err := load(bytes.NewReader(src))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return src, c, nil
}

func configErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, fmt.Sprintf(format, args...))
}
