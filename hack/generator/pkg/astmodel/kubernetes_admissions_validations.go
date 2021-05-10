/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 */

package astmodel

import (
	"go/token"

	"github.com/dave/dst"

	"github.com/Azure/k8s-infra/hack/generator/pkg/astbuilder"
)

func NewValidateResourceReferencesFunction(o *ObjectType, idFactory IdentifierFactory) *objectFunction {
	return &objectFunction{
		name:             "validateResourceReferences",
		o:                o,
		idFactory:        idFactory,
		asFunc:           validateResourceReferences,
		requiredPackages: NewPackageReferenceSet(GenRuntimeReference, ReflectHelpersReference),
	}
}

func validateResourceReferences(k *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *dst.FuncDecl {
	receiverIdent := k.idFactory.CreateIdentifier(receiver.Name(), NotExported)
	receiverType := receiver.AsType(codeGenerationContext)

	fn := &astbuilder.FuncDetails{
		Name:          methodName,
		ReceiverIdent: receiverIdent,
		ReceiverType: &dst.StarExpr{
			X: receiverType,
		},
		Returns: []*dst.Field{
			{
				Type: dst.NewIdent("error"),
			},
		},
		Body: validateResourceReferencesBody(codeGenerationContext, receiverIdent),
	}

	fn.AddComments("validates all resource references")
	return fn.DefineFunc()
}

func validateResourceReferencesBody(codeGenerationContext *CodeGenerationContext, receiverIdent string) []dst.Stmt {
	reflectHelpers, err := codeGenerationContext.GetImportedPackageName(ReflectHelpersReference)
	if err != nil {
		panic(err)
	}

	genRuntime, err := codeGenerationContext.GetImportedPackageName(GenRuntimeReference)
	if err != nil {
		panic(err)
	}
	var body []dst.Stmt

	body = append(
		body,
		astbuilder.SimpleAssignmentWithErr(
			dst.NewIdent("refs"),
			token.DEFINE,
			astbuilder.CallQualifiedFunc(
				reflectHelpers,
				"FindResourceReferences",
				astbuilder.AddrOf(astbuilder.Selector(dst.NewIdent(receiverIdent), "Spec")))))
	body = append(body, astbuilder.CheckErrorAndReturn())
	body = append(
		body,
		astbuilder.Returns(
			astbuilder.CallQualifiedFunc(
				genRuntime,
				"ValidateResourceReferences",
				dst.NewIdent("refs"))))

	return body
}
