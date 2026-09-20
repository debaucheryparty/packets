package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func (s EnvironmentSpec) Identity() EnvironmentID {
	var b strings.Builder

	comps := make([]string, len(s.Components))
	copy(comps, s.Components)
	sort.Strings(comps)

	tools := make([]string, len(s.Toolchains))
	copy(tools, s.Toolchains)
	sort.Strings(tools)

	b.WriteString(strings.Join(comps, ","))
	b.WriteString(":")
	b.WriteString(strings.Join(tools, ","))
	b.WriteString(":")
	b.WriteString(s.Target)
	b.WriteString(":")

	var versionKeys []string
	for k := range s.Versions {
		versionKeys = append(versionKeys, k)
	}
	sort.Strings(versionKeys)
	for _, k := range versionKeys {
		fmt.Fprintf(&b, "%s=%s;", k, s.Versions[k])
	}

	h := sha256.Sum256([]byte(b.String()))
	return EnvironmentID("env-" + hex.EncodeToString(h[:8]))
}

func (t *ProjectTopology) Spec() EnvironmentSpec {
	spec := EnvironmentSpec{
		Versions: make(map[string]string),
	}
	for _, c := range t.Components {
		spec.Components = append(spec.Components, string(c.Type))
		for _, r := range c.Requirements {
			spec.Toolchains = append(spec.Toolchains, r.Name)
			if r.Version != "" {
				spec.Versions[r.Name] = r.Version
			}
		}
	}
	return spec
}

func (t *ProjectTopology) Identity() EnvironmentID {
	return t.Spec().Identity()
}
