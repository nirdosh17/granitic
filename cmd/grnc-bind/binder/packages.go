// Copyright 2018 Granitic. All rights reserved.
// Use of this source code is governed by an Apache 2.0 license that can be found in the LICENSE file at the root of this project.
package binder

import (
	"github.com/graniticio/granitic/types"
	"github.com/pkg/errors"
	"strings"
)

func parsePackages(p []interface{}) (*packages, error) {

	seen := make(map[string]*packageInfo)
	dedupe := types.NewEmptyUnorderedStringSet()

	for _, iName := range p {

		var short string
		var full string
		alias := false

		fullName := iName.(string)

		if dedupe.Contains(fullName) {
			continue
		}

		dedupe.Add(fullName)

		if len(strings.TrimSpace(fullName)) == 0 {
			return nil, errors.Errorf("One of your package import statements is blank")
		}

		parts := strings.Split(fullName, " ")

		switch len(parts) {
		case 1:
			//No alias, extract package name
			elements := strings.Split(parts[0], "/")
			short = elements[len(elements)-1]
			full = parts[0]

		case 2:
			alias = true
			short = parts[0]
			full = parts[1]

		default:
			return nil, errors.Errorf("Too many spaces in %s", fullName)
		}

		if sp := seen[short]; sp == nil {
			pi := new(packageInfo)
			pi.Name = full
			pi.Unparsed = fullName

			if alias {
				pi.Alias = short
			}

			seen[short] = pi
		} else {

			return nil, errors.Errorf("Package definition \"%s\" clashes with \"%s\"", sp.Unparsed, fullName)

		}

	}

	parsed := new(packages)
	parsed.Info = seen

	return parsed, nil

}

type packages struct {
	Info map[string]*packageInfo
}

func (p *packages) Exists(s string) bool {
	return p.Info[s] != nil
}

type packageInfo struct {
	Name     string
	Alias    string
	Unparsed string
}
