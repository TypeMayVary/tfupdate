package tfupdate

import (
	"context"
	"path/filepath"
	"regexp"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
)

// moduleSourceRegexp is a regular expression for module source.
// This is not a complete module source definition, but it is sufficient to
// parse version. Note that a git reference can be branch name, so we need to
// check if it seems to be a version number.
// https://www.terraform.io/docs/modules/sources.html
var moduleSourceRegexp = regexp.MustCompile(`(.+)\?ref=([A-Za-z0-9_\/-][A-Za-z0-9._\/-]+)`)

// ModuleUpdater is a updater implementation which updates the module version constraint.
type ModuleUpdater struct {
	name      string
	nameRegex *regexp.Regexp
	version   string
	ref       string
}

// NewModuleUpdater is a factory method which returns an ModuleUpdater instance.
func NewModuleUpdater(name string, version string, ref string, nameRegex *regexp.Regexp) (Updater, error) {
	if len(name) == 0 {
		return nil, errors.Errorf("failed to new module updater. name is required")
	}

	if len(version) == 0 && len(ref) == 0 {
		return nil, errors.Errorf("failed to new module updater. version or ref is required")
	}

	return &ModuleUpdater{
		name:      name,
		nameRegex: nameRegex,
		version:   version,
		ref:       ref,
	}, nil
}

// Update updates the module version constraint.
// Note that this method will rewrite the AST passed as an argument.
func (u *ModuleUpdater) Update(_ context.Context, _ *ModuleContext, filename string, f *hclwrite.File) error {
	if filepath.Base(filename) == ".terraform.lock.hcl" {
		// skip a lock file.
		return nil
	}

	return u.updateModuleBlock(f)
}

func (u *ModuleUpdater) match(name string) bool {
	if u.nameRegex == nil {
		return u.name == name
	}
	return u.nameRegex.MatchString(name)
}

func (u *ModuleUpdater) updateModuleBlock(f *hclwrite.File) error {
	for _, m := range allMatchingBlocksByType(f.Body(), "module") {
		if s := m.Body().GetAttribute("source"); s != nil {
			name, version := parseModuleSource(s)
			// If this module is a target module
			if u.match(name) {
				if len(version) == 0 {
					// The source attribute doesn't have a version number.
					// Set a version to attribute value only if the version key exists.
					if m.Body().GetAttribute("version") != nil && len(u.version) != 0 {
						m.Body().SetAttributeValue("version", cty.StringVal(u.version))
					}
					continue
				}
				// The source attribute has a version number.
				// Update a version reference in the source value.
				var newSourceValue string

				if len(u.ref) != 0 {
					newSourceValue = name + `?ref=v` + u.version
				}

				if len(u.version) != 0 {
					newSourceValue = name + `?ref=` + u.ref
				}

				m.Body().SetAttributeValue("source", cty.StringVal(newSourceValue))
			}
		}
	}

	return nil
}

// parseModuleSource parses module source and returns module name and version.
func parseModuleSource(a *hclwrite.Attribute) (string, string) {
	tokens := a.Expr().BuildTokens(nil)
	if len(tokens) == 3 &&
		tokens[0].Type == hclsyntax.TokenOQuote &&
		tokens[1].Type == hclsyntax.TokenQuotedLit &&
		tokens[2].Type == hclsyntax.TokenCQuote {
		source := string(tokens[1].Bytes)
		matched := moduleSourceRegexp.FindStringSubmatch(source)
		if len(matched) == 0 {
			// no version number
			return source, ""
		}
		name := matched[1]
		version := matched[2]
		return name, version
	}
	return "", ""
}
