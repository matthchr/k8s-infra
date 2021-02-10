/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 */

package astmodel

import (
	"fmt"
	"go/token"

	"github.com/Azure/k8s-infra/hack/generator/pkg/astbuilder"
	"github.com/dave/dst"
	"github.com/pkg/errors"
)

// StorageTypeConversion generates the AST for a given conversion.
// reader is an expression to read the original value
// writer is an expression to write the converted value
// Both of these might be complex expressions, possibly involving indexing into arrays or maps.
type StorageTypeConversion func(reader dst.Expr, writer dst.Expr, generationContext *CodeGenerationContext) []dst.Stmt

// StorageTypeConversionFactory represents factory methods that can be used to create StorageTypeConversions
// for a specific pair of types
// source is the endpoint that will be read
// destination is the endpoint that will be written
type StorageTypeConversionFactory func(source *StorageConversionEndpoint, destination *StorageConversionEndpoint) StorageTypeConversion

// A list of all known type conversion factory methods
var typeConversionFactories []StorageTypeConversionFactory

func init() {
	typeConversionFactories = []StorageTypeConversionFactory{
		assignPrimitiveTypeFromPrimitiveType,
		assignOptionalPrimitiveTypeFromPrimitiveType,
		assignPrimitiveTypeFromOptionalPrimitiveType,
		assignOptionalPrimitiveTypeFromOptionalPrimitiveType,
		assignArrayFromArray,
		assignMapFromMap,
	}
}

// createTypeConversion tries to create a type conversion between the two provided types, using
// all of the available type conversion functions in priority order to do so.
func createTypeConversion(
	sourceEndpoint *StorageConversionEndpoint,
	destinationEndpoint *StorageConversionEndpoint) (StorageTypeConversion, error) {
	for _, f := range typeConversionFactories {
		result := f(sourceEndpoint, destinationEndpoint)
		if result != nil {
			return result, nil
		}
	}

	err := errors.Errorf(
		"no conversion found to assign %q from %q",
		destinationEndpoint.name,
		sourceEndpoint.name)

	return nil, err
}

// assignPrimitiveTypeFromPrimitiveType will generate a direct assignment if both types have the
// same underlying primitive type and are not optional
//
// <destination> = <source>
//
func assignPrimitiveTypeFromPrimitiveType(
	sourceEndpoint *StorageConversionEndpoint,
	destinationEndpoint *StorageConversionEndpoint) StorageTypeConversion {

	if _, srcOpt := AsOptionalType(sourceEndpoint.Type()); srcOpt {
		// Source is optional, which we handle elsewhere
		return nil
	}

	if _, dstOpt := AsOptionalType(destinationEndpoint.Type()); dstOpt {
		// Destination is optional, which we handle elsewhere
		return nil
	}

	srcPrim, srcOk := AsPrimitiveType(sourceEndpoint.Type())
	if !srcOk {
		// Source is not a primitive type
		return nil
	}

	dstPrim, dstOk := AsPrimitiveType(destinationEndpoint.Type())
	if !dstOk {
		// Destination is not a primitive type
		return nil
	}

	if !srcPrim.Equals(dstPrim) {
		// Not the same primitive type
		return nil
	}

	return func(reader dst.Expr, writer dst.Expr, generationContext *CodeGenerationContext) []dst.Stmt {
		return []dst.Stmt{
			astbuilder.SimpleAssignment(writer, token.ASSIGN, reader),
		}
	}
}

// assignOptionalPrimitiveTypeFromPrimitiveType will generate a direct assignment if both types
// have the same underlying primitive type and only the destination is optional.
//
// <destination> = &<source>
//
func assignOptionalPrimitiveTypeFromPrimitiveType(
	sourceEndpoint *StorageConversionEndpoint,
	destinationEndpoint *StorageConversionEndpoint) StorageTypeConversion {

	if _, srcOpt := AsOptionalType(sourceEndpoint.Type()); srcOpt {
		// Source is optional
		return nil
	}

	srcPrim, srcOk := AsPrimitiveType(sourceEndpoint.Type())
	if !srcOk {
		// Source is not a primitive type
		return nil
	}

	_, dstOpt := AsOptionalType(destinationEndpoint.Type())
	if !dstOpt {
		// Destination is not optional
		return nil
	}

	dstPrim, dstOk := AsPrimitiveType(destinationEndpoint.Type())
	if !dstOk {
		// Destination is not a primitive type
		return nil
	}

	if !srcPrim.Equals(dstPrim) {
		// Not the same primitive type
		return nil
	}

	return func(reader dst.Expr, writer dst.Expr, generationContext *CodeGenerationContext) []dst.Stmt {
		return []dst.Stmt{
			astbuilder.SimpleAssignment(writer, token.ASSIGN, astbuilder.AddrOf(reader)),
		}
	}
}

// assignPrimitiveTypeFromOptionalPrimitiveType will generate a direct assignment if both types
// have the same underlying primitive type and only the source is optional
//
// if <source> != nil {
//    <destination> = *<source>
// } else {
//    <destination> = <zero>
// }
func assignPrimitiveTypeFromOptionalPrimitiveType(
	sourceEndpoint *StorageConversionEndpoint,
	destinationEndpoint *StorageConversionEndpoint) StorageTypeConversion {

	if _, srcOpt := AsOptionalType(sourceEndpoint.Type()); !srcOpt {
		// Source is not optional
		return nil
	}

	srcPrim, srcOk := AsPrimitiveType(sourceEndpoint.Type())
	if !srcOk {
		// Source is not a primitive type
		return nil
	}

	if _, dstOpt := AsOptionalType(destinationEndpoint.Type()); dstOpt {
		// Destination is optional
		return nil
	}

	dstPrim, dstOk := AsPrimitiveType(destinationEndpoint.Type())
	if !dstOk {
		// Destination is not a primitive type
		return nil
	}

	if !srcPrim.Equals(dstPrim) {
		// Not the same primitive type
		return nil
	}

	return func(reader dst.Expr, writer dst.Expr, generationContext *CodeGenerationContext) []dst.Stmt {
		// Need to check for null and only assign if we have a value
		cond := astbuilder.NotEqual(reader, dst.NewIdent("nil"))

		assignValue := astbuilder.SimpleAssignment(writer, token.ASSIGN, astbuilder.Dereference(reader))

		assignZero := astbuilder.SimpleAssignment(
			writer,
			token.ASSIGN,
			&dst.BasicLit{
				Value: zeroValue(srcPrim),
			})

		stmt := &dst.IfStmt{
			Cond: cond,
			Body: &dst.BlockStmt{
				List: []dst.Stmt{
					assignValue,
				},
			},
			Else: &dst.BlockStmt{
				List: []dst.Stmt{
					assignZero,
				},
			},
		}

		return []dst.Stmt{stmt}
	}
}

// assignOptionalPrimitiveTypeFromOptionalPrimitiveType will generate a direct assignment if both types have the
// same underlying primitive type and both are optional
//
// <destination> = <source>
//
func assignOptionalPrimitiveTypeFromOptionalPrimitiveType(
	sourceEndpoint *StorageConversionEndpoint,
	destinationEndpoint *StorageConversionEndpoint) StorageTypeConversion {

	if _, srcOpt := AsOptionalType(sourceEndpoint.Type()); !srcOpt {
		// Source is not optional
		return nil
	}

	srcPrim, srcOk := AsPrimitiveType(sourceEndpoint.Type())
	if !srcOk {
		// Source is not a primitive type
		return nil
	}

	if _, dstOpt := AsOptionalType(destinationEndpoint.Type()); !dstOpt {
		// Destination is not optional
		return nil
	}

	dstPrim, dstOk := AsPrimitiveType(destinationEndpoint.Type())
	if !dstOk {
		// Destination is not a primitive type
		return nil
	}

	if !srcPrim.Equals(dstPrim) {
		// Not the same primitive type
		return nil
	}

	return func(reader dst.Expr, writer dst.Expr, ctx *CodeGenerationContext) []dst.Stmt {
		return []dst.Stmt{
			astbuilder.SimpleAssignment(writer, token.ASSIGN, reader),
		}
	}
}

// assignArrayFromArray will generate a code fragment to populate an array, assuming the
// underlying types of the two arrays are compatible
//
// <arr> := make([]<type>, len(<reader>))
// for <index>, <value> := range <reader> {
//     <arr>[<index>] := <value> // Or other conversion as required
// }
// <writer> = <arr>
//
func assignArrayFromArray(
	sourceEndpoint *StorageConversionEndpoint,
	destinationEndpoint *StorageConversionEndpoint) StorageTypeConversion {

	srcArray, srcOk := AsArrayType(sourceEndpoint.Type())
	if !srcOk {
		// Source is not an array
		return nil
	}

	dstArray, dstOk := AsArrayType(destinationEndpoint.Type())
	if !dstOk {
		// Destination is not an array
		return nil
	}

	srcEp := sourceEndpoint.WithType(srcArray.element)
	dstEp := destinationEndpoint.WithType(dstArray.element)
	conversion, _ := createTypeConversion(srcEp, dstEp)

	if conversion == nil {
		// No conversion between the elements of the array, so we can't do the conversion
		return nil
	}

	return func(reader dst.Expr, writer dst.Expr, generationContext *CodeGenerationContext) []dst.Stmt {
		itemId := sourceEndpoint.CreateSingularLocal()
		indexId := itemId + "Index"
		tempId := sourceEndpoint.CreatePluralLocal("List")

		declaration := astbuilder.SimpleAssignment(
			dst.NewIdent(tempId),
			token.DEFINE,
			astbuilder.MakeList(dstArray.AsType(generationContext), astbuilder.CallFunc("len", reader)))

		body := conversion(
			dst.NewIdent(itemId),
			&dst.IndexExpr{
				X:     dst.NewIdent(tempId),
				Index: dst.NewIdent(indexId),
			},
			generationContext,
		)

		assign := astbuilder.SimpleAssignment(writer, token.ASSIGN, dst.NewIdent(tempId))

		loop := astbuilder.IterateOverListWithIndex(indexId, itemId, reader, body...)
		loop.Decs.After = dst.EmptyLine

		return []dst.Stmt{
			declaration,
			loop,
			assign,
		}
	}
}

// assignMapFromMap will generate a code fragment to populate an array, assuming the
// underlying types of the two arrays are compatible
//
// <map> := make(map[<key>]<type>)
// for key, <item> := range <reader> {
//     <map>[<key>] := <item>
// }
// <writer> = <map>
//
func assignMapFromMap(
	sourceEndpoint *StorageConversionEndpoint,
	destinationEndpoint *StorageConversionEndpoint) StorageTypeConversion {
	srcMap, ok := AsMapType(sourceEndpoint.Type())
	if !ok {
		// Source is not a map
		return nil
	}

	dstMap, ok := AsMapType(destinationEndpoint.Type())
	if !ok {
		// Destination is not a map
		return nil
	}

	if !srcMap.key.Equals(dstMap.key) {
		// Keys are different types
		return nil
	}

	srcEp := sourceEndpoint.WithType(srcMap.value)
	dstEp := destinationEndpoint.WithType(dstMap.value)
	conversion, _ := createTypeConversion(srcEp, dstEp)

	if conversion == nil {
		// No conversion between the elements of the map, so we can't do the conversion
		return nil
	}

	return func(reader dst.Expr, writer dst.Expr, generationContext *CodeGenerationContext) []dst.Stmt {
		itemId := sourceEndpoint.CreateSingularLocal()
		keyId := itemId + "Key"
		tempId := sourceEndpoint.CreatePluralLocal("Map")

		declaration := astbuilder.SimpleAssignment(
			dst.NewIdent(tempId),
			token.DEFINE,
			astbuilder.MakeMap(dstMap.key.AsType(generationContext), dstMap.value.AsType(generationContext)))

		body := conversion(
			dst.NewIdent(itemId),
			&dst.IndexExpr{
				X:     dst.NewIdent(tempId),
				Index: dst.NewIdent(keyId),
			},
			generationContext,
		)

		assign := astbuilder.SimpleAssignment(writer, token.ASSIGN, dst.NewIdent(tempId))

		loop := astbuilder.IterateOverMapWithValue(keyId, itemId, reader, body...)
		loop.Decs.After = dst.EmptyLine

		return []dst.Stmt{
			declaration,
			loop,
			assign,
		}
	}
}

func zeroValue(p *PrimitiveType) string {
	switch p {
	case StringType:
		return "\"\""
	case IntType:
		return "0"
	case FloatType:
		return "0"
	case UInt32Type:
		return "0"
	case UInt64Type:
		return "0"
	case BoolType:
		return "false"
	default:
		panic(fmt.Sprintf("unexpected primitive type %q", p.String()))
	}
}