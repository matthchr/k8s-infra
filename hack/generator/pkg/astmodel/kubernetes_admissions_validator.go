/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 */

package astmodel

import (
	"fmt"
	"go/token"
	"strings"

	"github.com/dave/dst"

	"github.com/Azure/k8s-infra/hack/generator/pkg/astbuilder"
)

var ValidatorInterfaceName = MakeTypeName(ControllerRuntimeAdmission, "Validator")
var GenRuntimeValidatorInterfaceName = MakeTypeName(GenRuntimeReference, "Validator")

// ValidationKind determines when a particular validation should be run
type ValidationKind string

const (
	ValidationKindCreate = ValidationKind("Create")
	ValidationKindUpdate = ValidationKind("Update")
	ValidationKindDelete = ValidationKind("Delete")
)

// ValidatorBuilder helps in building an interface implementation for admissions.Validator.
type ValidatorBuilder struct {
	resourceName TypeName
	objectType   *ObjectType
	idFactory    IdentifierFactory

	validations map[ValidationKind][]*objectFunction
}

// NewValidatorBuilder creates a new ValidatorBuilder for the given object type.
func NewValidatorBuilder(resourceName TypeName, o *ObjectType, idFactory IdentifierFactory) *ValidatorBuilder {
	return &ValidatorBuilder{
		resourceName: resourceName,
		objectType:   o,
		idFactory:    idFactory,
		validations: map[ValidationKind][]*objectFunction{
			ValidationKindCreate: nil,
			ValidationKindUpdate: nil,
			ValidationKindDelete: nil,
		},
	}
}

// AddValidation adds an additional validation function to the set of validation functions to be applied to the given object.
func (v *ValidatorBuilder) AddValidation(kind ValidationKind, f *objectFunction) {
	if !v.objectType.Equals(f.o) {
		panic("cannot add validation function on non-matching object types")
	}
	v.validations[kind] = append(v.validations[kind], f)
}

// ToInterfaceImplementation creates an InterfaceImplementation that implements the admissions.Validator interface.
// This implementation includes calls to all validations registered with this ValidatorBuilder via the AddValidation function,
// as well as helper functions that allow additional handcrafted validations to be injected by
// implementing the genruntime.Validator interface.
func (v *ValidatorBuilder) ToInterfaceImplementation() *InterfaceImplementation {
	lpr, ok := v.resourceName.PackageReference.AsLocalPackage()
	if !ok {
		panic(fmt.Sprintf("expected resource name %s to be a local package reference", v.resourceName.String()))
	}

	group := lpr.group                // e.g. "microsoft.network.infra.azure.com"
	resource := v.resourceName.Name() // e.g. "backendaddresspools"
	version := lpr.version            // e.g. "v1"

	group = strings.ToLower(group + GroupSuffix)
	nonPluralResource := strings.ToLower(resource)
	resource = strings.ToLower(v.resourceName.Plural().Name())

	// e.g. "validate-microsoft-network-infra-azure-com-v1-backendaddresspool"
	// note that this must match _exactly_ how controller-runtime generates the path
	// or it will not work!
	path := fmt.Sprintf("/validate-%s-%s-%s", strings.ReplaceAll(group, ".", "-"), version, nonPluralResource)

	// e.g.  "default.v123.backendaddresspool.infra.azure.com"
	name := fmt.Sprintf("validate.%s.%s.%s", version, resource, group)

	annotation := fmt.Sprintf(
		"+kubebuilder:webhook:path=%s,mutating=false,sideEffects=None,"+
			"matchPolicy=Exact,failurePolicy=fail,groups=%s,resources=%s,"+
			"verbs=create;update,versions=%s,name=%s,admissionReviewVersions=v1beta1", // admission review version v1 is not yet supported by controller-runtime
		path,
		group,
		resource,
		version,
		name)

	funcs := []Function{
		&objectFunction{
			name:             "ValidateCreate",
			o:                v.objectType,
			idFactory:        v.idFactory,
			asFunc:           v.validateCreate,
			requiredPackages: NewPackageReferenceSet(GenRuntimeReference, APIMachineryErrorsReference, APIMachineryRuntimeReference),
		},
		&objectFunction{
			name:             "ValidateUpdate",
			o:                v.objectType,
			idFactory:        v.idFactory,
			asFunc:           v.validateUpdate,
			requiredPackages: NewPackageReferenceSet(GenRuntimeReference, APIMachineryErrorsReference, APIMachineryRuntimeReference),
		},
		&objectFunction{
			name:             "ValidateDelete",
			o:                v.objectType,
			idFactory:        v.idFactory,
			asFunc:           v.validateDelete,
			requiredPackages: NewPackageReferenceSet(GenRuntimeReference, APIMachineryErrorsReference, APIMachineryRuntimeReference),
		},
		&objectFunction{
			name:             "createValidations",
			o:                v.objectType,
			idFactory:        v.idFactory,
			asFunc:           v.localCreateValidations,
			requiredPackages: NewPackageReferenceSet(),
		},
		&objectFunction{
			name:             "updateValidations",
			o:                v.objectType,
			idFactory:        v.idFactory,
			asFunc:           v.localUpdateValidations,
			requiredPackages: NewPackageReferenceSet(),
		},
		&objectFunction{
			name:             "deleteValidations",
			o:                v.objectType,
			idFactory:        v.idFactory,
			asFunc:           v.localDeleteValidations,
			requiredPackages: NewPackageReferenceSet(),
		},
	}

	// Add the actual individual validation functions
	for _, validations := range v.validations {
		for _, validation := range validations {
			funcs = append(funcs, validation)
		}
	}

	return NewInterfaceImplementation(
		ValidatorInterfaceName,
		funcs...,
	).WithAnnotation(annotation)
}

// validateCreate returns a function that performs validation of creation for the resource
func (v *ValidatorBuilder) validateCreate(k *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *dst.FuncDecl {
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
		Body: v.validateBody(codeGenerationContext, receiverIdent, "createValidations", "CreateValidations", ""),
	}

	fn.AddComments("validates the creation of the resource")
	return fn.DefineFunc()
}

// validateUpdate returns a function that performs validation of update for the resource
func (v *ValidatorBuilder) validateUpdate(k *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *dst.FuncDecl {
	receiverIdent := k.idFactory.CreateIdentifier(receiver.Name(), NotExported)
	receiverType := receiver.AsType(codeGenerationContext)

	retType := getValidationFuncType(ValidationKindUpdate, codeGenerationContext)

	fn := &astbuilder.FuncDetails{
		Name:          methodName,
		Params:        retType.Params.List,
		ReceiverIdent: receiverIdent,
		ReceiverType: &dst.StarExpr{
			X: receiverType,
		},
		Returns: retType.Results.List,
		Body:    v.validateBody(codeGenerationContext, receiverIdent, "updateValidations", "UpdateValidations", "old"),
	}

	fn.AddComments("validates an update of the resource")
	return fn.DefineFunc()
}

// validateDelete returns a function that performs validation of deletion for the resource
func (v *ValidatorBuilder) validateDelete(k *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *dst.FuncDecl {
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
		Body: v.validateBody(codeGenerationContext, receiverIdent, "deleteValidations", "DeleteValidations", ""),
	}

	fn.AddComments("validates the deletion of the resource")
	return fn.DefineFunc()
}

func (v *ValidatorBuilder) validateBody(codeGenerationContext *CodeGenerationContext, receiverIdent string, implFunctionName string, overrideFunctionName string, funcParamIdent string) []dst.Stmt {
	kErrors, err := codeGenerationContext.GetImportedPackageName(APIMachineryErrorsReference)
	if err != nil {
		panic(err)
	}

	overrideInterfaceType := GenRuntimeValidatorInterfaceName.AsType(codeGenerationContext)

	validationsIdent := "validations"
	validationIdent := "validation"
	tempVarIdent := "temp"
	runtimeValidatorIdent := "runtimeValidator"
	errsIdent := "errs"

	var args []dst.Expr
	if funcParamIdent != "" {
		args = append(args, dst.NewIdent(funcParamIdent))
	}

	// TODO: This loop (and possibly some of the other body stuff below) could be done in a generic method written in
	// TODO: genruntime -- thoughts?
	validationLoop := &dst.RangeStmt{
		Key:   dst.NewIdent("_"),
		Value: dst.NewIdent(validationIdent),
		X:     dst.NewIdent(validationsIdent),
		Tok:   token.DEFINE,
		Body: &dst.BlockStmt{
			List: []dst.Stmt{
				astbuilder.SimpleAssignment(dst.NewIdent("err"), token.DEFINE, astbuilder.CallFunc(validationIdent, args...)),
				astbuilder.CheckErrorAndSingleStatement(astbuilder.AppendList(dst.NewIdent(errsIdent), dst.NewIdent("err"))),
			},
		},
	}

	hack := astbuilder.CallQualifiedFunc(runtimeValidatorIdent, overrideFunctionName)
	hack.Ellipsis = true

	appendFuncCall := astbuilder.CallFunc("append", dst.NewIdent(validationsIdent), astbuilder.CallQualifiedFunc(runtimeValidatorIdent, overrideFunctionName))
	appendFuncCall.Ellipsis = true

	body := []dst.Stmt{
		astbuilder.SimpleAssignment(
			dst.NewIdent(validationsIdent),
			token.DEFINE,
			astbuilder.CallQualifiedFunc(receiverIdent, implFunctionName)),
		astbuilder.AssignToInterface(tempVarIdent, dst.NewIdent(receiverIdent)),
		&dst.IfStmt{
			Init: astbuilder.TypeAssert(
				dst.NewIdent(runtimeValidatorIdent),
				dst.NewIdent(tempVarIdent),
				overrideInterfaceType),
			Cond: dst.NewIdent("ok"),
			Body: &dst.BlockStmt{
				List: []dst.Stmt{
					// Not using astbuilder.AppendList here as we want to tack on a "..." at the end
					astbuilder.SimpleAssignment(
						dst.NewIdent(validationsIdent),
						token.ASSIGN,
						appendFuncCall),
				},
			},
		},
		astbuilder.LocalVariableDeclaration(errsIdent, &dst.ArrayType{Elt: dst.NewIdent("error")}, ""),
		validationLoop,
		astbuilder.Returns(astbuilder.CallQualifiedFunc(kErrors, "NewAggregate", dst.NewIdent(errsIdent))),
	}

	return body
}

func (v *ValidatorBuilder) localCreateValidations(_ *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *dst.FuncDecl {
	fn := v.makeLocalValidationFuncDetails(ValidationKindCreate, codeGenerationContext, receiver, methodName)
	fn.AddComments("validates the creation of the resource")
	return fn.DefineFunc()
}

func (v *ValidatorBuilder) localUpdateValidations(_ *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *dst.FuncDecl {
	fn := v.makeLocalValidationFuncDetails(ValidationKindUpdate, codeGenerationContext, receiver, methodName)
	fn.AddComments("validates the update of the resource")
	return fn.DefineFunc()
}

func (v *ValidatorBuilder) localDeleteValidations(_ *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *dst.FuncDecl {
	fn := v.makeLocalValidationFuncDetails(ValidationKindDelete, codeGenerationContext, receiver, methodName)
	fn.AddComments("validates the deletion of the resource")
	return fn.DefineFunc()
}

func (v *ValidatorBuilder) makeLocalValidationFuncDetails(kind ValidationKind, codeGenerationContext *CodeGenerationContext, receiver TypeName, methodName string) *astbuilder.FuncDetails {
	receiverIdent := v.idFactory.CreateIdentifier(receiver.Name(), NotExported)
	receiverType := receiver.AsType(codeGenerationContext)

	return &astbuilder.FuncDetails{
		Name:          methodName,
		ReceiverIdent: receiverIdent,
		ReceiverType: &dst.StarExpr{
			X: receiverType,
		},
		Returns: []*dst.Field{
			{
				Type: &dst.ArrayType{
					Elt: getValidationFuncType(kind, codeGenerationContext),
				},
			},
		},
		Body: v.localValidationFuncBody(kind, codeGenerationContext, receiver),
	}
}

func (v *ValidatorBuilder) localValidationFuncBody(kind ValidationKind, codeGenerationContext *CodeGenerationContext, receiver TypeName) []dst.Stmt {
	var elements []dst.Expr
	for _, validationFunc := range v.validations[kind] {
		elements = append(elements, v.makeLocalValidationElement(kind, validationFunc, codeGenerationContext, receiver))
	}

	if len(elements) == 0 {
		return []dst.Stmt{astbuilder.Returns(dst.NewIdent("nil"))}
	}

	returnStmt := astbuilder.Returns(&dst.CompositeLit{
		Type: &dst.ArrayType{
			Elt: getValidationFuncType(kind, codeGenerationContext),
		},
		Elts: elements,
	})

	return []dst.Stmt{returnStmt}
}

func (v *ValidatorBuilder) makeLocalValidationElement(kind ValidationKind, validation *objectFunction, codeGenerationContext *CodeGenerationContext, receiver TypeName) dst.Expr {
	receiverIdent := v.idFactory.CreateIdentifier(receiver.Name(), NotExported)

	if kind == ValidationKindUpdate {
		// It's common that updates don't actually need the "old" variable. If the function that we're going to be calling
		// doesn't take any parameters, provide a wrapper
		f := validation.asFunc(validation, codeGenerationContext, receiver, validation.name)
		if f.Type.Params.NumFields() == 0 {
			return &dst.FuncLit{
				Decs: dst.FuncLitDecorations{
					NodeDecs: dst.NodeDecs{
						//Start:  doc,
						Before: dst.NewLine,
						After:  dst.NewLine,
					},
				},
				Type: getValidationFuncType(kind, codeGenerationContext),
				Body: &dst.BlockStmt{
					List: []dst.Stmt{
						astbuilder.Returns(astbuilder.CallQualifiedFunc(receiverIdent, validation.name)),
					},
				},
			}
		}
	}

	return astbuilder.Selector(dst.NewIdent(receiverIdent), validation.name)
}

func getValidationFuncType(kind ValidationKind, codeGenerationContext *CodeGenerationContext) *dst.FuncType {
	runtime, err := codeGenerationContext.GetImportedPackageName(APIMachineryRuntimeReference)
	if err != nil {
		panic(err)
	}

	if kind == ValidationKindUpdate {
		return &dst.FuncType{
			Params: &dst.FieldList{
				List: []*dst.Field{
					{
						Names: []*dst.Ident{
							dst.NewIdent("old"),
						},
						Type: &dst.SelectorExpr{
							X:   dst.NewIdent(runtime),
							Sel: dst.NewIdent("Object"),
						},
					},
				},
			},
			Results: &dst.FieldList{
				List: []*dst.Field{
					{
						Type: dst.NewIdent("error"),
					},
				},
			},
		}
	}

	return &dst.FuncType{
		Results: &dst.FieldList{
			List: []*dst.Field{
				{
					Type: dst.NewIdent("error"),
				},
			},
		},
	}
}
