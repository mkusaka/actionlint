package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v4"
)

// IgnorePatterns is a list of regular expressions. These patterns are used for filtering errors by
// matching the error messages.
type IgnorePatterns []*regexp.Regexp

// Match returns whether the given error should be ignored due to the "ignore" configuration.
func (pats IgnorePatterns) Match(err *Error) bool {
	for _, r := range pats {
		if r.MatchString(err.Message) {
			return true
		}
	}
	return false
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (pats *IgnorePatterns) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("yaml: \"ignore\" must be a sequence node at line:%d,col:%d", n.Line, n.Column)
	}
	rs := make([]*regexp.Regexp, 0, len(n.Content))
	for _, p := range n.Content {
		r, err := regexp.Compile(p.Value)
		if err != nil {
			return fmt.Errorf("invalid regular expression %q in \"ignore\" at line%d,col:%d: %w", p.Value, n.Line, n.Column, err)
		}
		rs = append(rs, r)
	}
	*pats = rs
	return nil
}

// PathConfig is a configuration for specific file path pattern. This is for values of the "paths" mapping
// in the configuration file.
type PathConfig struct {
	// Ignore is a list of patterns. They are used for ignoring errors by matching to the error messages.
	// It is similar to the "-ignore" command line option.
	Ignore IgnorePatterns `yaml:"ignore"`
}

// TimeoutMinutesConfig specifies an optional job timeout policy.
type TimeoutMinutesConfig struct {
	Required   bool    `yaml:"required"`
	MaxMinutes float64 `yaml:"max"`
}

const (
	AssumeDefaultPermissionsRestricted = "restricted"
	AssumeDefaultPermissionsPermissive = "permissive"
)

// Config is configuration of yactionlint. This struct instance is parsed from "yactionlint.yaml"
// file usually put in ".github" directory.
type Config struct {
	// SelfHostedRunner is configuration for self-hosted runner.
	SelfHostedRunner struct {
		// Labels is label names for self-hosted runner.
		Labels []string `yaml:"labels"`
	} `yaml:"self-hosted-runner"`
	// ConfigVariables is names of configuration variables used in the checked workflows. When this value is nil,
	// property names of `vars` context will not be checked. Otherwise actionlint will report a name which is not
	// listed here as undefined config variables.
	// https://docs.github.com/en/actions/learn-github-actions/variables
	ConfigVariables []string `yaml:"config-variables"`
	// ConfigSecrets is an optional allow-list of repository and organization secrets.
	// Nil disables this check; an empty sequence permits no non-built-in secrets.
	ConfigSecrets []string `yaml:"config-secrets"`
	// RequiredActions specifies actions which must be used in each workflow.
	RequiredActions []RequiredActionRule `yaml:"required-actions"`
	// RequireCommitHash requires repository actions to use full-length commit SHA references.
	RequireCommitHash bool `yaml:"require-commit-hash"`
	// RequireExactActionVersion requires immutable commit hashes or exact semantic version tags.
	RequireExactActionVersion bool `yaml:"require-exact-action-version"`
	// RequireExplicitIfExpressions requires ${{ ... }} around optional job and step if expressions.
	RequireExplicitIfExpressions bool `yaml:"require-explicit-if-expressions"`
	// RequirePermissions requires a workflow-level permissions declaration.
	RequirePermissions bool `yaml:"require-permissions"`
	// RequireExplicitPermissions enforces per-job permissions and least-privilege workflow defaults.
	RequireExplicitPermissions bool `yaml:"require-explicit-permissions"`
	// Paths is a "paths" mapping in the configuration file. The keys are glob patterns to match file paths.
	// And the values are corresponding configurations applied to the file paths.
	Paths map[string]PathConfig `yaml:"paths"`
	// TimeoutMinutes configures required and maximum job timeouts.
	TimeoutMinutes TimeoutMinutesConfig `yaml:"timeout-minutes"`
	// AssumeDefaultPermissions models the caller token when no permissions are declared.
	AssumeDefaultPermissions *string `yaml:"assume-default-permissions"`
}

// PathConfigs returns a list of all PathConfig values matching to the given file path. The path must
// be relative to the root of the project.
func (cfg *Config) PathConfigs(path string) []PathConfig {
	path = filepath.ToSlash(path)

	var ret []PathConfig
	if cfg != nil {
		for p, c := range cfg.Paths {
			// Glob patterns were validated in `ParseConfig()`
			if doublestar.MatchUnvalidated(p, path) {
				ret = append(ret, c)
			}
		}
	}
	return ret
}

// ParseConfig parses the given bytes as a yactionlint config file. When deserializing the YAML file
// or the config validation fails, this function returns an error.
func ParseConfig(b []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " ")
		return nil, errors.New(msg)
	}
	for pat := range c.Paths {
		if !doublestar.ValidatePattern(pat) {
			return nil, fmt.Errorf("invalid glob pattern %q in \"paths\"", pat)
		}
	}
	if c.TimeoutMinutes.MaxMinutes < 0 {
		return nil, fmt.Errorf("\"timeout-minutes.max\" must not be negative")
	}
	if c.AssumeDefaultPermissions != nil {
		switch *c.AssumeDefaultPermissions {
		case AssumeDefaultPermissionsRestricted, AssumeDefaultPermissionsPermissive:
		default:
			return nil, fmt.Errorf("invalid value %q for \"assume-default-permissions\": expected %q or %q", *c.AssumeDefaultPermissions, AssumeDefaultPermissionsRestricted, AssumeDefaultPermissionsPermissive)
		}
	}
	return &c, nil
}

// ReadConfigFile reads yactionlint config file (yactionlint.yaml) from the given file path.
func ReadConfigFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file %q: %w", path, err)
	}
	c, err := ParseConfig(b)
	if err != nil {
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	return c, nil
}

// loadRepoConfig reads config file from the repository's .github/yactionlint.yml or
// .github/yactionlint.yaml.
func loadRepoConfig(root string) (*Config, error) {
	for _, f := range []string{"yactionlint.yaml", "yactionlint.yml"} {
		p := filepath.Join(root, ".github", f)
		c, err := ReadConfigFile(p)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			return nil, fmt.Errorf("could not parse config file %q: %w", p, err)
		default:
			return c, nil
		}
	}
	return nil, nil
}

func writeDefaultConfigFile(path string) error {
	b := []byte(`self-hosted-runner:
  # Labels of self-hosted runner in array of strings.
  labels: []

# Configuration variables in array of strings defined in your repository or
# organization. ` + "`null`" + ` means disabling configuration variables check.
# Empty array means no configuration variable is allowed.
config-variables: null

# Secrets defined in the repository or organization. null disables checking.
# An empty array disallows all non-built-in secrets.
config-secrets: null

# Configuration for file paths. The keys are glob patterns to match to file
# paths relative to the repository root. The values are the configurations for
# the file paths. Note that the path separator is always '/'.
# The following configurations are available.
#
# "ignore" is an array of regular expression patterns. Matched error messages
# are ignored. This is similar to the "-ignore" command line option.
paths:
#  .github/workflows/**/*.yml:
#    ignore: []

# Optional policy for job timeout-minutes. max: 0 disables the maximum check.
#timeout-minutes:
#  required: false
#  max: 60

# Caller token assumption when no permissions are declared: restricted or permissive.
#assume-default-permissions: restricted
`)
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("could not write default configuration file at %q: %w", path, err)
	}
	return nil
}
