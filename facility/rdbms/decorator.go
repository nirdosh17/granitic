// Copyright 2016-2018 Granitic. All rights reserved.
// Use of this source code is governed by an Apache 2.0 license that can be found in the LICENSE file at the root of this project.

package rdbms

import (
	"github.com/graniticio/granitic/ioc"
	"github.com/graniticio/granitic/rdbms"
	"github.com/graniticio/granitic/reflecttools"
	"reflect"
)

type clientManagerDecorator struct {
	fieldNameManager map[string]rdbms.RdbmsClientManager
}

func (cmd *clientManagerDecorator) OfInterest(component *ioc.Component) bool {

	result := false

	for field, manager := range cmd.fieldNameManager {

		i := component.Instance

		if fieldPresent := reflecttools.HasFieldOfName(i, field); !fieldPresent {
			continue
		}

		targetFieldType := reflecttools.TypeOfField(i, field)
		managerType := reflect.TypeOf(manager)

		v := reflect.ValueOf(i).Elem().FieldByName(field)

		if managerType.AssignableTo(targetFieldType) && v.IsNil() {
			return true
		}
	}

	return result
}

func (cmd *clientManagerDecorator) DecorateComponent(component *ioc.Component, container *ioc.ComponentContainer) {

	for field, manager := range cmd.fieldNameManager {

		i := component.Instance

		if fieldPresent := reflecttools.HasFieldOfName(i, field); !fieldPresent {
			continue
		}

		targetFieldType := reflecttools.TypeOfField(i, field)
		managerType := reflect.TypeOf(manager)

		v := reflect.ValueOf(i).Elem().FieldByName(field)

		if managerType.AssignableTo(targetFieldType) && v.IsNil() {
			rc := reflect.ValueOf(i).Elem()
			rc.FieldByName(field).Set(reflect.ValueOf(manager))
		}
	}

}
