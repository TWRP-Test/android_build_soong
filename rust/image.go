// Copyright 2020 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rust

import (
	"strings"

	"android/soong/android"
	"android/soong/cc"
)

var _ android.ImageInterface = (*Module)(nil)

var _ cc.ImageMutatableModule = (*Module)(nil)

func (mod *Module) VendorAvailable() bool {
	return Bool(mod.VendorProperties.Vendor_available)
}

func (mod *Module) OdmAvailable() bool {
	return Bool(mod.VendorProperties.Odm_available)
}

func (mod *Module) ProductAvailable() bool {
	return Bool(mod.VendorProperties.Product_available)
}

func (mod *Module) RamdiskAvailable() bool {
	return Bool(mod.Properties.Ramdisk_available)
}

func (mod *Module) VendorRamdiskAvailable() bool {
	return Bool(mod.Properties.Vendor_ramdisk_available)
}

func (mod *Module) AndroidModuleBase() *android.ModuleBase {
	return &mod.ModuleBase
}

func (mod *Module) RecoveryAvailable() bool {
	return Bool(mod.Properties.Recovery_available)
}

func (mod *Module) ExtraVariants() []string {
	return mod.Properties.ExtraVariants
}

func (mod *Module) AppendExtraVariant(extraVariant string) {
	mod.Properties.ExtraVariants = append(mod.Properties.ExtraVariants, extraVariant)
}

func (mod *Module) SetRamdiskVariantNeeded(b bool) {
	mod.Properties.RamdiskVariantNeeded = b
}

func (mod *Module) SetVendorRamdiskVariantNeeded(b bool) {
	mod.Properties.VendorRamdiskVariantNeeded = b
}

func (mod *Module) SetRecoveryVariantNeeded(b bool) {
	mod.Properties.RecoveryVariantNeeded = b
}

func (mod *Module) SetCoreVariantNeeded(b bool) {
	mod.Properties.CoreVariantNeeded = b
}

func (mod *Module) SetProductVariantNeeded(b bool) {
	mod.Properties.ProductVariantNeeded = b
}

func (mod *Module) SetVendorVariantNeeded(b bool) {
	mod.Properties.VendorVariantNeeded = b
}

func (mod *Module) SnapshotVersion(mctx android.ImageInterfaceContext) string {
	if snapshot, ok := mod.compiler.(cc.SnapshotInterface); ok {
		return snapshot.Version()
	} else {
		mctx.ModuleErrorf("version is unknown for snapshot prebuilt")
		return ""
	}
}

func (mod *Module) VendorVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return mod.Properties.VendorVariantNeeded
}

func (mod *Module) ProductVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return mod.Properties.ProductVariantNeeded
}

func (mod *Module) VendorRamdiskVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return mod.Properties.VendorRamdiskVariantNeeded
}

func (mod *Module) CoreVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return mod.Properties.CoreVariantNeeded
}

func (mod *Module) RamdiskVariantNeeded(android.ImageInterfaceContext) bool {
	return mod.Properties.RamdiskVariantNeeded
}

func (mod *Module) DebugRamdiskVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return false
}

func (mod *Module) RecoveryVariantNeeded(android.ImageInterfaceContext) bool {
	return mod.Properties.RecoveryVariantNeeded
}

func (mod *Module) ExtraImageVariations(android.ImageInterfaceContext) []string {
	return mod.Properties.ExtraVariants
}

func (mod *Module) IsSnapshotPrebuilt() bool {
	if p, ok := mod.compiler.(cc.SnapshotInterface); ok {
		return p.IsSnapshotPrebuilt()
	}
	return false
}

func (mod *Module) InstallInVendor() bool {
	// Additionally check if this module is inVendor() that means it is a "vendor" variant of a
	// module. As well as SoC specific modules, vendor variants must be installed to /vendor
	// unless they have "odm_available: true".
	return mod.HasVendorVariant() && mod.InVendor() && !mod.VendorVariantToOdm()
}

func (mod *Module) InstallInOdm() bool {
	// Some vendor variants want to be installed to /odm by setting "odm_available: true".
	return mod.InVendor() && mod.VendorVariantToOdm()
}

// Returns true when this module creates a vendor variant and wants to install the vendor variant
// to the odm partition.
func (c *Module) VendorVariantToOdm() bool {
	return Bool(c.VendorProperties.Odm_available)
}

func (ctx *moduleContext) ProductSpecific() bool {
	return ctx.ModuleContext.ProductSpecific() || ctx.RustModule().productSpecificModuleContext()
}

func (c *Module) productSpecificModuleContext() bool {
	// Additionally check if this module is inProduct() that means it is a "product" variant of a
	// module. As well as product specific modules, product variants must be installed to /product.
	return c.InProduct()
}

func (mod *Module) InRecovery() bool {
	return mod.ModuleBase.InRecovery() || mod.ModuleBase.InstallInRecovery()
}

func (mod *Module) InRamdisk() bool {
	return mod.ModuleBase.InRamdisk() || mod.ModuleBase.InstallInRamdisk()
}

func (mod *Module) InVendorRamdisk() bool {
	return mod.ModuleBase.InVendorRamdisk() || mod.ModuleBase.InstallInVendorRamdisk()
}

func (mod *Module) OnlyInRamdisk() bool {
	return mod.ModuleBase.InstallInRamdisk()
}

func (mod *Module) OnlyInRecovery() bool {
	return mod.ModuleBase.InstallInRecovery()
}

func (mod *Module) OnlyInVendorRamdisk() bool {
	return mod.ModuleBase.InstallInVendorRamdisk()
}

// Returns true when this module is configured to have core and vendor variants.
func (mod *Module) HasVendorVariant() bool {
	return Bool(mod.VendorProperties.Vendor_available) || Bool(mod.VendorProperties.Odm_available)
}

// Always returns false because rust modules do not support product variant.
func (mod *Module) HasProductVariant() bool {
	return Bool(mod.VendorProperties.Product_available)
}

func (mod *Module) HasNonSystemVariants() bool {
	return mod.HasVendorVariant() || mod.HasProductVariant()
}

func (mod *Module) InProduct() bool {
	return mod.Properties.ImageVariation == android.ProductVariation
}

// Returns true if the module is "vendor" variant. Usually these modules are installed in /vendor
func (mod *Module) InVendor() bool {
	return mod.Properties.ImageVariation == android.VendorVariation
}

// Returns true if the module is "vendor" or "product" variant.
func (mod *Module) InVendorOrProduct() bool {
	return mod.InVendor() || mod.InProduct()
}

func (mod *Module) SetImageVariation(ctx android.ImageInterfaceContext, variant string) {
	if variant == android.VendorRamdiskVariation {
		mod.MakeAsPlatform()
	} else if variant == android.RecoveryVariation {
		mod.MakeAsPlatform()
	} else if strings.HasPrefix(variant, android.VendorVariation) {
		mod.Properties.ImageVariation = android.VendorVariation
		if strings.HasPrefix(variant, cc.VendorVariationPrefix) {
			mod.Properties.VndkVersion = strings.TrimPrefix(variant, cc.VendorVariationPrefix)
		}
	} else if strings.HasPrefix(variant, android.ProductVariation) {
		mod.Properties.ImageVariation = android.ProductVariation
		if strings.HasPrefix(variant, cc.ProductVariationPrefix) {
			mod.Properties.VndkVersion = strings.TrimPrefix(variant, cc.ProductVariationPrefix)
		}
	}
}

func (mod *Module) ImageMutatorBegin(mctx android.ImageInterfaceContext) {
	if Bool(mod.VendorProperties.Double_loadable) {
		mctx.PropertyErrorf("double_loadable",
			"Rust modules do not yet support double loading")
	}
	if Bool(mod.Properties.Vendor_ramdisk_available) {
		if lib, ok := mod.compiler.(libraryInterface); !ok || (ok && lib.buildShared()) {
			mctx.PropertyErrorf("vendor_ramdisk_available", "cannot be set for rust_ffi or rust_ffi_shared modules.")
		}
	}

	cc.MutateImage(mctx, mod)

	if !mod.Properties.CoreVariantNeeded || mod.HasNonSystemVariants() {

		if _, ok := mod.compiler.(*prebuiltLibraryDecorator); ok {
			// Rust does not support prebuilt libraries on non-System images.
			mctx.ModuleErrorf("Rust prebuilt modules not supported for non-system images.")
		}
	}
}
