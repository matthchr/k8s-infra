/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 */

package codegen

import (
	"context"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/Azure/k8s-infra/hack/generator/pkg/astmodel"
)

// fixOptionalAliasReferences fixes up properties on objects that are optional and aliases to types such as arrays/maps that should not be optional.
// This cannot easily be detected by the basic handling on OptionalType itself (or in PropertyDefinition) due to the requirement to follow the
// typeName link and examine it.
func fixOptionalAliasReferences() PipelineStage {
	return MakePipelineStage(
		"fixOptionalAliasReferences",
		"Fixes optional alias references to ensure that they are not optional if the reference is to an array or map",
		func(ctx context.Context, definitions astmodel.Types) (astmodel.Types, error) {
			visitor := astmodel.MakeTypeVisitor()
			var result = make(astmodel.Types)

			visitor.VisitObjectType = func(this *astmodel.TypeVisitor, it *astmodel.ObjectType, ctx interface{}) (astmodel.Type, error) {
				return fixOptionalAliasProperties(definitions, it)
			}

			var errs []error
			for _, typeDef := range definitions {
				visitedType, err := visitor.Visit(typeDef.Type(), nil)
				if err != nil {
					errs = append(errs, err)
				} else {
					result.Add(typeDef.WithType(visitedType))
				}
			}

			if len(errs) > 0 {
				return nil, kerrors.NewAggregate(errs)
			}

			return result, nil
		})
}

func isAliasToOptionalType(definitions astmodel.Types, typeName astmodel.TypeName) (bool, error) {
	// Some type names aren't local, skip checking these
	if _, ok := typeName.PackageReference.AsLocalPackage(); !ok {
		return false, nil
	}

	def, ok := definitions[typeName]
	if !ok {
		return false, errors.Errorf("couldn't find definition for %v", typeName)
	}

	_, isArray := astmodel.AsArrayType(def.Type())
	_, isMap := astmodel.AsMapType(def.Type())

	// TODO: Do we need to handle recursive case where this is also a typename?

	return isArray || isMap, nil
}

func fixOptionalAliasProperties(definitions astmodel.Types, it *astmodel.ObjectType) (astmodel.Type, error) {
	var errs []error
	var newProps []*astmodel.PropertyDefinition
	for _, prop := range it.Properties() {

		if prop.IsRequired() {
			// No change here - type isn't optional to begin with
			newProps = append(newProps, prop)
		}

		typeName, ok := astmodel.AsTypeName(prop.PropertyType())
		if !ok {
			// This type is something weird, just preserve it
			newProps = append(newProps, prop)
			continue
		}

		isAliasToOptional, err := isAliasToOptionalType(definitions, typeName)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if isAliasToOptional {
			// TODO: Doing this actually has outcomes we don't like... I think we want to just remove the optional type but
			// TODO: leave required = false?
			newProps = append(newProps, prop.MakeRequired())
		}
	}

	if len(errs) > 0 {
		return nil, kerrors.NewAggregate(errs)
	}

	result := it.WithProperties(newProps...)

	return result, nil
}
