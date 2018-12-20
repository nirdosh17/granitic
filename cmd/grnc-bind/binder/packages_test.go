package binder

import (
	"github.com/graniticio/granitic/test"
	"testing"
)

func TestValid(t *testing.T) {

	s := []interface{}{"github.com/graniticio/granitic/cmd/grnc-bind/binder",
		"github.com/graniticio/granitic/config",
		"github.com/graniticio/granitic/ioc",
		"github.com/graniticio/granitic/logging",
		"testing"}

	pi, err := parsePackages(s)

	test.ExpectNil(t, err)

	pl := len(pi.Info)

	test.ExpectInt(t, pl, 5)

}

func TestDeduping(t *testing.T) {

	s := []interface{}{"github.com/graniticio/granitic/cmd/grnc-bind/binder",
		"github.com/graniticio/granitic/cmd/grnc-bind/binder"}

	pi, err := parsePackages(s)

	test.ExpectNil(t, err)

	pl := len(pi.Info)

	test.ExpectInt(t, pl, 1)

}

func TestEmptyPackage(t *testing.T) {

	s := []interface{}{" "}

	_, err := parsePackages(s)

	test.ExpectNotNil(t, err)

}

func TestTooManySpaces(t *testing.T) {

	s := []interface{}{"a l github.com/graniticio/granitic/config"}

	_, err := parsePackages(s)

	test.ExpectNotNil(t, err)

}

func TestAllowedConflict(t *testing.T) {

	s := []interface{}{"github.com/graniticio/granitic/config", "cfg github.com/graniticio/granitic/config"}

	_, err := parsePackages(s)

	test.ExpectNil(t, err)

}

func TestAliasConflictsWithPackage(t *testing.T) {

	s := []interface{}{"github.com/graniticio/granitic/cfg", "cfg github.com/graniticio/granitic/config"}

	_, err := parsePackages(s)

	test.ExpectNotNil(t, err)

}
