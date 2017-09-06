// Copyright 2015 Google Inc. All rights reserved.
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

package java

// This file contains the module types for compiling Java for Android, and converts the properties
// into the flags and filenames necessary to pass to the Module.  The final creation of the rules
// is handled in builder.go

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/genrule"
	"android/soong/java/config"
)

func init() {
	android.RegisterModuleType("java_defaults", defaultsFactory)

	android.RegisterModuleType("java_library", LibraryFactory)
	android.RegisterModuleType("java_library_static", LibraryFactory)
	android.RegisterModuleType("java_library_host", LibraryHostFactory)
	android.RegisterModuleType("java_binary", BinaryFactory)
	android.RegisterModuleType("java_binary_host", BinaryHostFactory)
	android.RegisterModuleType("java_import", ImportFactory)
	android.RegisterModuleType("java_import_host", ImportFactoryHost)
	android.RegisterModuleType("android_prebuilt_sdk", SdkPrebuiltFactory)
	android.RegisterModuleType("android_app", AndroidAppFactory)

	android.RegisterSingletonType("logtags", LogtagsSingleton)
}

// TODO:
// Autogenerated files:
//  Proto
//  Renderscript
// Post-jar passes:
//  Proguard
//  Jacoco
//  Jarjar
//  Dex
// Rmtypedefs
// DroidDoc
// Findbugs

type CompilerProperties struct {
	// list of source files used to compile the Java module.  May be .java, .logtags, .proto,
	// or .aidl files.
	Srcs []string `android:"arch_variant"`

	// list of source files that should not be used to build the Java module.
	// This is most useful in the arch/multilib variants to remove non-common files
	Exclude_srcs []string `android:"arch_variant"`

	// list of directories containing Java resources
	Resource_dirs []string `android:"arch_variant"`

	// list of directories that should be excluded from resource_dirs
	Exclude_resource_dirs []string `android:"arch_variant"`

	// don't build against the default libraries (legacy-test, core-junit,
	// ext, and framework for device targets)
	No_standard_libs *bool

	// list of module-specific flags that will be used for javac compiles
	Javacflags []string `android:"arch_variant"`

	// list of of java libraries that will be in the classpath
	Libs []string `android:"arch_variant"`

	// list of java libraries that will be compiled into the resulting jar
	Static_libs []string `android:"arch_variant"`

	// manifest file to be included in resulting jar
	Manifest *string

	// if not blank, run jarjar using the specified rules file
	Jarjar_rules *string

	// If not blank, set the java version passed to javac as -source and -target
	Java_version *string

	// If set to false, don't allow this module to be installed.  Defaults to true.
	Installable *bool
}

type CompilerDeviceProperties struct {
	// list of module-specific flags that will be used for dex compiles
	Dxflags []string `android:"arch_variant"`

	// if not blank, set to the version of the sdk to compile against
	Sdk_version string

	// Set for device java libraries, and for host versions of device java libraries
	// built for testing
	Dex bool `blueprint:"mutated"`

	// directories to pass to aidl tool
	Aidl_includes []string

	// directories that should be added as include directories
	// for any aidl sources of modules that depend on this module
	Export_aidl_include_dirs []string
}

// Module contains the properties and members used by all java module types
type Module struct {
	android.ModuleBase
	android.DefaultableModuleBase

	properties       CompilerProperties
	deviceProperties CompilerDeviceProperties

	// output file suitable for inserting into the classpath of another compile
	classpathFile android.Path

	// output file suitable for installing or running
	outputFile android.Path

	exportAidlIncludeDirs android.Paths

	logtagsSrcs android.Paths

	// filelists of extra source files that should be included in the javac command line,
	// for example R.java generated by aapt for android apps
	ExtraSrcLists android.Paths

	// installed file for binary dependency
	installFile android.Path
}

type Dependency interface {
	ClasspathFiles() android.Paths
	AidlIncludeDirs() android.Paths
}

func InitJavaModule(module android.DefaultableModule, hod android.HostOrDeviceSupported) {
	android.InitAndroidArchModule(module, hod, android.MultilibCommon)
	android.InitDefaultableModule(module)
}

type dependencyTag struct {
	blueprint.BaseDependencyTag
	name string
}

var (
	staticLibTag     = dependencyTag{name: "staticlib"}
	libTag           = dependencyTag{name: "javalib"}
	bootClasspathTag = dependencyTag{name: "bootclasspath"}
	frameworkResTag  = dependencyTag{name: "framework-res"}
	sdkDependencyTag = dependencyTag{name: "sdk"}
)

func (j *Module) deps(ctx android.BottomUpMutatorContext) {
	if !proptools.Bool(j.properties.No_standard_libs) {
		if ctx.Device() {
			switch j.deviceProperties.Sdk_version {
			case "":
				ctx.AddDependency(ctx.Module(), bootClasspathTag, "core-oj", "core-libart")
				ctx.AddDependency(ctx.Module(), libTag, config.DefaultLibraries...)
			case "current":
				// TODO: !TARGET_BUILD_APPS
				// TODO: export preprocessed framework.aidl from android_stubs_current
				ctx.AddDependency(ctx.Module(), bootClasspathTag, "android_stubs_current")
			case "test_current":
				ctx.AddDependency(ctx.Module(), bootClasspathTag, "android_test_stubs_current")
			case "system_current":
				ctx.AddDependency(ctx.Module(), bootClasspathTag, "android_system_stubs_current")
			default:
				ctx.AddDependency(ctx.Module(), sdkDependencyTag, "sdk_v"+j.deviceProperties.Sdk_version)
			}
		} else {
			if j.deviceProperties.Dex {
				ctx.AddDependency(ctx.Module(), bootClasspathTag, "core-oj", "core-libart")
			}
		}
	}
	ctx.AddDependency(ctx.Module(), libTag, j.properties.Libs...)
	ctx.AddDependency(ctx.Module(), staticLibTag, j.properties.Static_libs...)

	android.ExtractSourcesDeps(ctx, j.properties.Srcs)
}

func (j *Module) aidlFlags(ctx android.ModuleContext, aidlPreprocess android.OptionalPath,
	aidlIncludeDirs android.Paths) []string {

	localAidlIncludes := android.PathsForModuleSrc(ctx, j.deviceProperties.Aidl_includes)

	var flags []string
	if aidlPreprocess.Valid() {
		flags = append(flags, "-p"+aidlPreprocess.String())
	} else {
		flags = append(flags, android.JoinWithPrefix(aidlIncludeDirs.Strings(), "-I"))
	}

	flags = append(flags, android.JoinWithPrefix(j.exportAidlIncludeDirs.Strings(), "-I"))
	flags = append(flags, android.JoinWithPrefix(localAidlIncludes.Strings(), "-I"))
	flags = append(flags, "-I"+android.PathForModuleSrc(ctx).String())
	if src := android.ExistentPathForSource(ctx, "", "src"); src.Valid() {
		flags = append(flags, "-I"+src.String())
	}

	return flags
}

func (j *Module) collectDeps(ctx android.ModuleContext) (classpath, bootClasspath, staticJars,
	aidlIncludeDirs, srcFileLists android.Paths, aidlPreprocess android.OptionalPath) {

	ctx.VisitDirectDeps(func(module blueprint.Module) {
		otherName := ctx.OtherModuleName(module)
		tag := ctx.OtherModuleDependencyTag(module)

		dep, _ := module.(Dependency)
		if dep == nil {
			switch tag {
			case android.DefaultsDepTag, android.SourceDepTag:
			default:
				ctx.ModuleErrorf("depends on non-java module %q", otherName)
			}
			return
		}

		switch tag {
		case bootClasspathTag:
			bootClasspath = append(bootClasspath, dep.ClasspathFiles()...)
		case libTag:
			classpath = append(classpath, dep.ClasspathFiles()...)
		case staticLibTag:
			classpath = append(classpath, dep.ClasspathFiles()...)
			staticJars = append(staticJars, dep.ClasspathFiles()...)
		case frameworkResTag:
			if ctx.ModuleName() == "framework" {
				// framework.jar has a one-off dependency on the R.java and Manifest.java files
				// generated by framework-res.apk
				srcFileLists = append(srcFileLists, module.(*AndroidApp).aaptJavaFileList)
			}
		case sdkDependencyTag:
			sdkDep := module.(sdkDependency)
			bootClasspath = append(bootClasspath, sdkDep.ClasspathFiles()...)
			if sdkDep.AidlPreprocessed().Valid() {
				if aidlPreprocess.Valid() {
					ctx.ModuleErrorf("multiple dependencies with preprocessed aidls:\n %q\n %q",
						aidlPreprocess, sdkDep.AidlPreprocessed())
				} else {
					aidlPreprocess = sdkDep.AidlPreprocessed()
				}
			}
		default:
			panic(fmt.Errorf("unknown dependency %q for %q", otherName, ctx.ModuleName()))
		}

		aidlIncludeDirs = append(aidlIncludeDirs, dep.AidlIncludeDirs()...)
	})

	return
}

func (j *Module) compile(ctx android.ModuleContext) {

	j.exportAidlIncludeDirs = android.PathsForModuleSrc(ctx, j.deviceProperties.Export_aidl_include_dirs)

	classpath, bootClasspath, staticJars, aidlIncludeDirs, srcFileLists,
		aidlPreprocess := j.collectDeps(ctx)

	var flags javaBuilderFlags

	javacFlags := j.properties.Javacflags

	if j.properties.Java_version != nil {
		flags.javaVersion = *j.properties.Java_version
	} else {
		flags.javaVersion = "${config.DefaultJavaVersion}"
	}

	if len(javacFlags) > 0 {
		ctx.Variable(pctx, "javacFlags", strings.Join(javacFlags, " "))
		flags.javacFlags = "$javacFlags"
	}

	aidlFlags := j.aidlFlags(ctx, aidlPreprocess, aidlIncludeDirs)
	if len(aidlFlags) > 0 {
		ctx.Variable(pctx, "aidlFlags", strings.Join(aidlFlags, " "))
		flags.aidlFlags = "$aidlFlags"
	}

	var deps android.Paths

	if len(bootClasspath) > 0 {
		flags.bootClasspath = "-bootclasspath " + strings.Join(bootClasspath.Strings(), ":")
		deps = append(deps, bootClasspath...)
	} else if ctx.Device() {
		// Explicitly clear the bootclasspath for device builds
		flags.bootClasspath = `-bootclasspath ""`
	}

	if len(classpath) > 0 {
		flags.classpath = "-classpath " + strings.Join(classpath.Strings(), ":")
		deps = append(deps, classpath...)
	}

	srcFiles := ctx.ExpandSources(j.properties.Srcs, j.properties.Exclude_srcs)

	srcFiles = j.genSources(ctx, srcFiles, flags)

	ctx.VisitDirectDeps(func(module blueprint.Module) {
		if gen, ok := module.(genrule.SourceFileGenerator); ok {
			srcFiles = append(srcFiles, gen.GeneratedSourceFiles()...)
		}
	})

	srcFileLists = append(srcFileLists, j.ExtraSrcLists...)

	var extraJarDeps android.Paths

	var jars android.Paths

	if len(srcFiles) > 0 {
		// Compile java sources into .class files
		classes := TransformJavaToClasses(ctx, srcFiles, srcFileLists, flags, deps)
		if ctx.Failed() {
			return
		}

		if ctx.AConfig().IsEnvTrue("RUN_ERROR_PRONE") {
			// If error-prone is enabled, add an additional rule to compile the java files into
			// a separate set of classes (so that they don't overwrite the normal ones and require
			// a rebuild when error-prone is turned off).  Add the classes as a dependency to
			// the jar command so the two compiles can run in parallel.
			// TODO(ccross): Once we always compile with javac9 we may be able to conditionally
			//    enable error-prone without affecting the output class files.
			errorprone := RunErrorProne(ctx, srcFiles, srcFileLists, flags, deps)
			extraJarDeps = append(extraJarDeps, errorprone)
		}

		jars = append(jars, classes)
	}

	resourceJarSpecs := ResourceDirsToJarSpecs(ctx, j.properties.Resource_dirs, j.properties.Exclude_resource_dirs)
	manifest := android.OptionalPathForModuleSrc(ctx, j.properties.Manifest)

	if len(resourceJarSpecs) > 0 || manifest.Valid() {
		// Combine classes + resources into classes-full-debug.jar
		resourceJar := TransformResourcesToJar(ctx, resourceJarSpecs, manifest, extraJarDeps)
		if ctx.Failed() {
			return
		}

		jars = append(jars, resourceJar)
	}

	jars = append(jars, staticJars...)

	// Combine the classes built from sources, any manifests, and any static libraries into
	// classes-combined.jar.  If there is only one input jar this step will be skipped.
	outputFile := TransformJarsToJar(ctx, "classes-combined.jar", jars)

	if j.properties.Jarjar_rules != nil {
		jarjar_rules := android.PathForModuleSrc(ctx, *j.properties.Jarjar_rules)
		// Transform classes-combined.jar into classes-jarjar.jar
		outputFile = TransformJarJar(ctx, outputFile, jarjar_rules)
		if ctx.Failed() {
			return
		}
	}

	j.classpathFile = outputFile

	if j.deviceProperties.Dex && len(srcFiles) > 0 {
		dxFlags := j.deviceProperties.Dxflags
		if false /* emma enabled */ {
			// If you instrument class files that have local variable debug information in
			// them emma does not correctly maintain the local variable table.
			// This will cause an error when you try to convert the class files for Android.
			// The workaround here is to build different dex file here based on emma switch
			// then later copy into classes.dex. When emma is on, dx is run with --no-locals
			// option to remove local variable information
			dxFlags = append(dxFlags, "--no-locals")
		}

		if ctx.AConfig().Getenv("NO_OPTIMIZE_DX") != "" {
			dxFlags = append(dxFlags, "--no-optimize")
		}

		if ctx.AConfig().Getenv("GENERATE_DEX_DEBUG") != "" {
			dxFlags = append(dxFlags,
				"--debug",
				"--verbose",
				"--dump-to="+android.PathForModuleOut(ctx, "classes.lst").String(),
				"--dump-width=1000")
		}

		var minSdkVersion string
		switch j.deviceProperties.Sdk_version {
		case "", "current", "test_current", "system_current":
			minSdkVersion = strconv.Itoa(ctx.AConfig().DefaultAppTargetSdkInt())
		default:
			minSdkVersion = j.deviceProperties.Sdk_version
		}

		dxFlags = append(dxFlags, "--min-sdk-version="+minSdkVersion)

		flags.dxFlags = strings.Join(dxFlags, " ")

		// Compile classes.jar into classes.dex
		dexJarSpec := TransformClassesJarToDex(ctx, outputFile, flags)
		if ctx.Failed() {
			return
		}

		// Combine classes.dex + resources into javalib.jar
		outputFile = TransformDexToJavaLib(ctx, resourceJarSpecs, dexJarSpec)
	}
	ctx.CheckbuildFile(outputFile)
	j.outputFile = outputFile
}

var _ Dependency = (*Library)(nil)

func (j *Module) ClasspathFiles() android.Paths {
	return android.Paths{j.classpathFile}
}

func (j *Module) AidlIncludeDirs() android.Paths {
	return j.exportAidlIncludeDirs
}

var _ logtagsProducer = (*Module)(nil)

func (j *Module) logtags() android.Paths {
	return j.logtagsSrcs
}

//
// Java libraries (.jar file)
//

type Library struct {
	Module
}

func (j *Library) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	j.compile(ctx)

	if j.properties.Installable == nil || *j.properties.Installable == true {
		j.installFile = ctx.InstallFile(android.PathForModuleInstall(ctx, "framework"),
			ctx.ModuleName()+".jar", j.outputFile)
	}
}

func (j *Library) DepsMutator(ctx android.BottomUpMutatorContext) {
	j.deps(ctx)
}

func LibraryFactory() android.Module {
	module := &Library{}

	module.deviceProperties.Dex = true

	module.AddProperties(
		&module.Module.properties,
		&module.Module.deviceProperties)

	InitJavaModule(module, android.HostAndDeviceSupported)
	return module
}

func LibraryHostFactory() android.Module {
	module := &Library{}

	module.AddProperties(&module.Module.properties)

	InitJavaModule(module, android.HostSupported)
	return module
}

//
// Java Binaries (.jar file plus wrapper script)
//

type binaryProperties struct {
	// installable script to execute the resulting jar
	Wrapper string
}

type Binary struct {
	Library

	binaryProperties binaryProperties

	wrapperFile android.ModuleSrcPath
	binaryFile  android.OutputPath
}

func (j *Binary) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	j.Library.GenerateAndroidBuildActions(ctx)

	// Depend on the installed jar (j.installFile) so that the wrapper doesn't get executed by
	// another build rule before the jar has been installed.
	j.wrapperFile = android.PathForModuleSrc(ctx, j.binaryProperties.Wrapper)
	j.binaryFile = ctx.InstallExecutable(android.PathForModuleInstall(ctx, "bin"),
		ctx.ModuleName(), j.wrapperFile, j.installFile)
}

func (j *Binary) DepsMutator(ctx android.BottomUpMutatorContext) {
	j.deps(ctx)
}

func BinaryFactory() android.Module {
	module := &Binary{}

	module.deviceProperties.Dex = true

	module.AddProperties(
		&module.Module.properties,
		&module.Module.deviceProperties,
		&module.binaryProperties)

	InitJavaModule(module, android.HostAndDeviceSupported)
	return module
}

func BinaryHostFactory() android.Module {
	module := &Binary{}

	module.AddProperties(
		&module.Module.properties,
		&module.Module.deviceProperties,
		&module.binaryProperties)

	InitJavaModule(module, android.HostSupported)
	return module
}

//
// Java prebuilts
//

type ImportProperties struct {
	Jars []string
}

type Import struct {
	android.ModuleBase
	prebuilt android.Prebuilt

	properties ImportProperties

	classpathFiles        android.Paths
	combinedClasspathFile android.Path
}

func (j *Import) Prebuilt() *android.Prebuilt {
	return &j.prebuilt
}

func (j *Import) PrebuiltSrcs() []string {
	return j.properties.Jars
}

func (j *Import) Name() string {
	return j.prebuilt.Name(j.ModuleBase.Name())
}

func (j *Import) DepsMutator(ctx android.BottomUpMutatorContext) {
}

func (j *Import) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	j.classpathFiles = android.PathsForModuleSrc(ctx, j.properties.Jars)

	j.combinedClasspathFile = TransformJarsToJar(ctx, "classes.jar", j.classpathFiles)
}

var _ Dependency = (*Import)(nil)

func (j *Import) ClasspathFiles() android.Paths {
	return j.classpathFiles
}

func (j *Import) AidlIncludeDirs() android.Paths {
	return nil
}

var _ android.PrebuiltInterface = (*Import)(nil)

func ImportFactory() android.Module {
	module := &Import{}

	module.AddProperties(&module.properties)

	android.InitPrebuiltModule(module, &module.properties.Jars)
	android.InitAndroidArchModule(module, android.HostAndDeviceSupported, android.MultilibCommon)
	return module
}

func ImportFactoryHost() android.Module {
	module := &Import{}

	module.AddProperties(&module.properties)

	android.InitPrebuiltModule(module, &module.properties.Jars)
	android.InitAndroidArchModule(module, android.HostSupported, android.MultilibCommon)
	return module
}

//
// SDK java prebuilts (.jar containing resources plus framework.aidl)
//

type sdkDependency interface {
	Dependency
	AidlPreprocessed() android.OptionalPath
}

var _ sdkDependency = (*sdkPrebuilt)(nil)

type sdkPrebuiltProperties struct {
	Aidl_preprocessed *string
}

type sdkPrebuilt struct {
	Import

	sdkProperties sdkPrebuiltProperties

	aidlPreprocessed android.OptionalPath
}

func (j *sdkPrebuilt) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	j.Import.GenerateAndroidBuildActions(ctx)

	j.aidlPreprocessed = android.OptionalPathForModuleSrc(ctx, j.sdkProperties.Aidl_preprocessed)
}

func (j *sdkPrebuilt) AidlPreprocessed() android.OptionalPath {
	return j.aidlPreprocessed
}

func SdkPrebuiltFactory() android.Module {
	module := &sdkPrebuilt{}

	module.AddProperties(&module.sdkProperties)

	android.InitPrebuiltModule(module, &module.properties.Jars)
	android.InitAndroidArchModule(module, android.HostAndDeviceSupported, android.MultilibCommon)
	return module
}

func inList(s string, l []string) bool {
	for _, e := range l {
		if e == s {
			return true
		}
	}
	return false
}

//
// Defaults
//
type Defaults struct {
	android.ModuleBase
	android.DefaultsModuleBase
}

func (*Defaults) GenerateAndroidBuildActions(ctx android.ModuleContext) {
}

func (d *Defaults) DepsMutator(ctx android.BottomUpMutatorContext) {
}

func defaultsFactory() android.Module {
	return DefaultsFactory()
}

func DefaultsFactory(props ...interface{}) android.Module {
	module := &Defaults{}

	module.AddProperties(props...)
	module.AddProperties(
		&CompilerProperties{},
		&CompilerDeviceProperties{},
	)

	android.InitDefaultsModule(module)

	return module
}
