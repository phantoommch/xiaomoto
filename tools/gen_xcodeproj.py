#!/usr/bin/env python3
"""Generate Motocam.xcodeproj with stable IDs. Run from the repo root."""

import hashlib
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
os.chdir(ROOT)


def uid(text: str) -> str:
    return hashlib.sha1(text.encode()).hexdigest()[:24].upper()


def files_under(folder: str, suffix: str):
    base = ROOT / folder
    found = []
    for path in sorted(base.rglob(f"*{suffix}")):
        if path.is_file():
            found.append(path.relative_to(ROOT).as_posix())
    return found


app_sources = files_under("Motocam", ".swift")
test_sources = files_under("MotocamTests", ".swift")
resources = [
    "Motocam/Assets.xcassets",
    "Motocam/PrivacyInfo.xcprivacy",
]
plist = "Motocam/Info.plist"
fixture = "MotocamTests/Fixtures/ticket_chishi.txt"
configs = [
    "Config/Debug.xcconfig",
    "Config/Release.xcconfig",
    "Config/Shared.xcconfig",
    "Config/Secrets.example.xcconfig",
]

app_target = uid("target:Motocam")
test_target = uid("target:MotocamTests")
project_id = uid("project")
app_product = uid("product:Motocam.app")
test_product = uid("product:MotocamTests.xctest")
app_sources_phase = uid("phase:app-sources")
test_sources_phase = uid("phase:test-sources")
app_resources_phase = uid("phase:app-resources")
app_frameworks_phase = uid("phase:app-frameworks")
test_frameworks_phase = uid("phase:test-frameworks")
project_config_list = uid("configlist:project")
app_config_list = uid("configlist:app")
test_config_list = uid("configlist:test")
debug_project = uid("config:project:Debug")
release_project = uid("config:project:Release")
debug_app = uid("config:app:Debug")
release_app = uid("config:app:Release")
debug_test = uid("config:test:Debug")
release_test = uid("config:test:Release")
debug_xcconfig = uid("fileref:Config/Debug.xcconfig")
release_xcconfig = uid("fileref:Config/Release.xcconfig")


def file_ref(path: str) -> str:
    return uid("fileref:" + path)


def build_file(phase: str, path: str) -> str:
    return uid(f"build:{phase}:{path}")


refs = {}
for path in app_sources + test_sources + resources + configs + [plist, fixture]:
    refs[path] = file_ref(path)

group_main = uid("group:main")
group_motocam = uid("group:Motocam")
group_core = uid("group:Core")
group_app = uid("group:App")
group_views = uid("group:Views")
group_tests = uid("group:tests")
group_fixtures = uid("group:fixtures")
group_config = uid("group:config")
group_products = uid("group:products")


def group_children(paths):
    return "\n".join(f"\t\t\t\t{refs[p]}," for p in paths)


core_paths = [p for p in app_sources if p.startswith("Motocam/Core/")]
app_paths = [p for p in app_sources if p.startswith("Motocam/App/")]
view_paths = [p for p in app_sources if p.startswith("Motocam/Views/")]

build_lines = []
for path in app_sources:
    build_lines.append(
        f"\t\t{build_file('app', path)} /* {Path(path).name} in Sources */ = {{isa = PBXBuildFile; fileRef = {refs[path]} /* {Path(path).name} */; }};"
    )
for path in test_sources:
    build_lines.append(
        f"\t\t{build_file('test', path)} /* {Path(path).name} in Sources */ = {{isa = PBXBuildFile; fileRef = {refs[path]} /* {Path(path).name} */; }};"
    )
for path in resources:
    build_lines.append(
        f"\t\t{build_file('res', path)} /* {Path(path).name} in Resources */ = {{isa = PBXBuildFile; fileRef = {refs[path]} /* {Path(path).name} */; }};"
    )

file_ref_lines = []
for path in app_sources + test_sources + [plist, fixture] + configs:
    name = Path(path).name
    file_ref_lines.append(
        f"\t\t{refs[path]} /* {name} */ = {{isa = PBXFileReference; lastKnownFileType = text; path = {name}; sourceTree = \"<group>\"; }};"
    )
file_ref_lines.append(
    f"\t\t{refs['Motocam/Assets.xcassets']} /* Assets.xcassets */ = {{isa = PBXFileReference; lastKnownFileType = folder.assetcatalog; path = Assets.xcassets; sourceTree = \"<group>\"; }};"
)
file_ref_lines.append(
    f"\t\t{refs['Motocam/PrivacyInfo.xcprivacy']} /* PrivacyInfo.xcprivacy */ = {{isa = PBXFileReference; lastKnownFileType = text.xml; path = PrivacyInfo.xcprivacy; sourceTree = \"<group>\"; }};"
)
file_ref_lines.append(
    f"\t\t{app_product} /* Motocam.app */ = {{isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = Motocam.app; sourceTree = BUILT_PRODUCTS_DIR; }};"
)
file_ref_lines.append(
    f"\t\t{test_product} /* MotocamTests.xctest */ = {{isa = PBXFileReference; explicitFileType = wrapper.cfbundle; includeInIndex = 0; path = MotocamTests.xctest; sourceTree = BUILT_PRODUCTS_DIR; }};"
)

# Swift files should be sourcecode.swift, plist text.plist.xml
file_ref_lines = []
for path in app_sources + test_sources:
    name = Path(path).name
    file_ref_lines.append(
        f"\t\t{refs[path]} /* {name} */ = {{isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = {name}; sourceTree = \"<group>\"; }};"
    )
file_ref_lines.append(
    f"\t\t{refs[plist]} /* Info.plist */ = {{isa = PBXFileReference; lastKnownFileType = text.plist.xml; path = Info.plist; sourceTree = \"<group>\"; }};"
)
file_ref_lines.append(
    f"\t\t{refs[fixture]} /* ticket_chishi.txt */ = {{isa = PBXFileReference; lastKnownFileType = text; path = ticket_chishi.txt; sourceTree = \"<group>\"; }};"
)
for path in configs:
    name = Path(path).name
    file_ref_lines.append(
        f"\t\t{refs[path]} /* {name} */ = {{isa = PBXFileReference; lastKnownFileType = text.xcconfig; path = {name}; sourceTree = \"<group>\"; }};"
    )
file_ref_lines.append(
    f"\t\t{refs['Motocam/Assets.xcassets']} /* Assets.xcassets */ = {{isa = PBXFileReference; lastKnownFileType = folder.assetcatalog; path = Assets.xcassets; sourceTree = \"<group>\"; }};"
)
file_ref_lines.append(
    f"\t\t{refs['Motocam/PrivacyInfo.xcprivacy']} /* PrivacyInfo.xcprivacy */ = {{isa = PBXFileReference; lastKnownFileType = text.xml; path = PrivacyInfo.xcprivacy; sourceTree = \"<group>\"; }};"
)
file_ref_lines.append(
    f"\t\t{app_product} /* Motocam.app */ = {{isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = Motocam.app; sourceTree = BUILT_PRODUCTS_DIR; }};"
)
file_ref_lines.append(
    f"\t\t{test_product} /* MotocamTests.xctest */ = {{isa = PBXFileReference; explicitFileType = wrapper.cfbundle; includeInIndex = 0; path = MotocamTests.xctest; sourceTree = BUILT_PRODUCTS_DIR; }};"
)


def children(ids_and_comments):
    body = "\n".join(f"\t\t\t\t{i} /* {c} */," for i, c in ids_and_comments)
    return body


core_children = children([(refs[p], Path(p).name) for p in core_paths])
app_children = children([(refs[p], Path(p).name) for p in app_paths])
view_children = children([(refs[p], Path(p).name) for p in view_paths])
test_children = children([(refs[p], Path(p).name) for p in test_sources] + [(group_fixtures, "Fixtures")])
fixture_children = children([(refs[fixture], "ticket_chishi.txt")])
config_children = children([(refs[p], Path(p).name) for p in configs])
motocam_children = children(
    [(group_app, "App"), (group_core, "Core"), (group_views, "Views"), (refs["Motocam/Assets.xcassets"], "Assets.xcassets"), (refs[plist], "Info.plist"), (refs["Motocam/PrivacyInfo.xcprivacy"], "PrivacyInfo.xcprivacy")]
)
main_children = children(
    [(group_config, "Config"), (group_motocam, "Motocam"), (group_tests, "MotocamTests"), (group_products, "Products")]
)
product_children = children([(app_product, "Motocam.app"), (test_product, "MotocamTests.xctest")])

app_source_files = "\n".join(
    f"\t\t\t\t{build_file('app', p)} /* {Path(p).name} in Sources */," for p in app_sources
)
test_source_files = "\n".join(
    f"\t\t\t\t{build_file('test', p)} /* {Path(p).name} in Sources */," for p in test_sources
)
resource_files = "\n".join(
    f"\t\t\t\t{build_file('res', p)} /* {Path(p).name} in Resources */," for p in resources
)

common_target = """
				ASSETCATALOG_COMPILER_APPICON_NAME = AppIcon;
				ASSETCATALOG_COMPILER_GLOBAL_ACCENT_COLOR_NAME = AccentColor;
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = 1;
				IPHONEOS_DEPLOYMENT_TARGET = 17.0;
				ENABLE_PREVIEWS = YES;
				GENERATE_INFOPLIST_FILE = YES;
				INFOPLIST_FILE = Motocam/Info.plist;
				INFOPLIST_KEY_UIApplicationSceneManifest_Generation = YES;
				INFOPLIST_KEY_UIApplicationSupportsIndirectInputEvents = YES;
				INFOPLIST_KEY_UISupportedInterfaceOrientations_iPad = "UIInterfaceOrientationPortrait UIInterfaceOrientationPortraitUpsideDown UIInterfaceOrientationLandscapeLeft UIInterfaceOrientationLandscapeRight";
				INFOPLIST_KEY_UISupportedInterfaceOrientations_iPhone = "UIInterfaceOrientationPortrait UIInterfaceOrientationLandscapeLeft UIInterfaceOrientationLandscapeRight";
				LD_RUNPATH_SEARCH_PATHS = (
					"$(inherited)",
					"@executable_path/Frameworks",
				);
				INFOPLIST_KEY_CFBundleDisplayName = "小moto安全驾驶指南";
				INFOPLIST_KEY_CFBundleName = "小moto安全驾驶指南";
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = com.example.xiaomoto;
				PRODUCT_NAME = "$(TARGET_NAME)";
				SWIFT_EMIT_LOC_STRINGS = YES;
				SWIFT_STRICT_CONCURRENCY = minimal;
				SWIFT_VERSION = 5.0;
				TARGETED_DEVICE_FAMILY = "1,2";
"""

test_settings = """
				BUNDLE_LOADER = "$(TEST_HOST)";
				CODE_SIGN_STYLE = Automatic;
				CURRENT_PROJECT_VERSION = 1;
				GENERATE_INFOPLIST_FILE = YES;
				IPHONEOS_DEPLOYMENT_TARGET = 17.0;
				MARKETING_VERSION = 1.0;
				PRODUCT_BUNDLE_IDENTIFIER = com.example.xiaomoto.tests;
				PRODUCT_NAME = "$(TARGET_NAME)";
				SWIFT_EMIT_LOC_STRINGS = NO;
				SWIFT_STRICT_CONCURRENCY = minimal;
				SWIFT_VERSION = 5.0;
				TARGETED_DEVICE_FAMILY = "1,2";
				TEST_HOST = "$(BUILT_PRODUCTS_DIR)/Motocam.app/$(BUNDLE_EXECUTABLE_FOLDER_PATH)/Motocam";
"""

project_common = """
				ALWAYS_SEARCH_USER_PATHS = NO;
				CLANG_ANALYZER_NONNULL = YES;
				CLANG_ENABLE_MODULES = YES;
				CLANG_ENABLE_OBJC_ARC = YES;
				COPY_PHASE_STRIP = NO;
				ENABLE_STRICT_OBJC_MSGSEND = YES;
				GCC_C_LANGUAGE_STANDARD = gnu17;
				GCC_NO_COMMON_BLOCKS = YES;
				IPHONEOS_DEPLOYMENT_TARGET = 17.0;
				SDKROOT = iphoneos;
				STRING_CATALOG_GENERATE_SYMBOLS = NO;
				SWIFT_VERSION = 5.0;
				TARGETED_DEVICE_FAMILY = "1,2";
"""

pbx = f"""// !$*UTF8*$!
{{
	archiveVersion = 1;
	classes = {{
	}};
	objectVersion = 56;
	objects = {{

/* Begin PBXBuildFile section */
{chr(10).join(build_lines)}
/* End PBXBuildFile section */

/* Begin PBXContainerItemProxy section */
		{uid("proxy:test")} /* PBXContainerItemProxy */ = {{
			isa = PBXContainerItemProxy;
			containerPortal = {project_id} /* Project object */;
			proxyType = 1;
			remoteGlobalIDString = {app_target};
			remoteInfo = Motocam;
		}};
/* End PBXContainerItemProxy section */

/* Begin PBXFileReference section */
{chr(10).join(file_ref_lines)}
/* End PBXFileReference section */

/* Begin PBXFrameworksBuildPhase section */
		{app_frameworks_phase} /* Frameworks */ = {{
			isa = PBXFrameworksBuildPhase;
			buildActionMask = 2147483647;
			files = (
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};
		{test_frameworks_phase} /* Frameworks */ = {{
			isa = PBXFrameworksBuildPhase;
			buildActionMask = 2147483647;
			files = (
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};
/* End PBXFrameworksBuildPhase section */

/* Begin PBXGroup section */
		{group_app} /* App */ = {{
			isa = PBXGroup;
			children = (
{app_children}
			);
			path = App;
			sourceTree = "<group>";
		}};
		{group_core} /* Core */ = {{
			isa = PBXGroup;
			children = (
{core_children}
			);
			path = Core;
			sourceTree = "<group>";
		}};
		{group_views} /* Views */ = {{
			isa = PBXGroup;
			children = (
{view_children}
			);
			path = Views;
			sourceTree = "<group>";
		}};
		{group_motocam} /* Motocam */ = {{
			isa = PBXGroup;
			children = (
{motocam_children}
			);
			path = Motocam;
			sourceTree = "<group>";
		}};
		{group_fixtures} /* Fixtures */ = {{
			isa = PBXGroup;
			children = (
{fixture_children}
			);
			path = Fixtures;
			sourceTree = "<group>";
		}};
		{group_tests} /* MotocamTests */ = {{
			isa = PBXGroup;
			children = (
{test_children}
			);
			path = MotocamTests;
			sourceTree = "<group>";
		}};
		{group_config} /* Config */ = {{
			isa = PBXGroup;
			children = (
{config_children}
			);
			path = Config;
			sourceTree = "<group>";
		}};
		{group_products} /* Products */ = {{
			isa = PBXGroup;
			children = (
{product_children}
			);
			name = Products;
			sourceTree = "<group>";
		}};
		{group_main} = {{
			isa = PBXGroup;
			children = (
{main_children}
			);
			sourceTree = "<group>";
		}};
/* End PBXGroup section */

/* Begin PBXNativeTarget section */
		{app_target} /* Motocam */ = {{
			isa = PBXNativeTarget;
			buildConfigurationList = {app_config_list} /* Build configuration list for PBXNativeTarget "Motocam" */;
			buildPhases = (
				{app_sources_phase} /* Sources */,
				{app_frameworks_phase} /* Frameworks */,
				{app_resources_phase} /* Resources */,
			);
			buildRules = (
			);
			dependencies = (
			);
			name = Motocam;
			productName = Motocam;
			productReference = {app_product} /* Motocam.app */;
			productType = "com.apple.product-type.application";
		}};
		{test_target} /* MotocamTests */ = {{
			isa = PBXNativeTarget;
			buildConfigurationList = {test_config_list} /* Build configuration list for PBXNativeTarget "MotocamTests" */;
			buildPhases = (
				{test_sources_phase} /* Sources */,
				{test_frameworks_phase} /* Frameworks */,
			);
			buildRules = (
			);
			dependencies = (
				{uid("dep:test")} /* PBXTargetDependency */,
			);
			name = MotocamTests;
			productName = MotocamTests;
			productReference = {test_product} /* MotocamTests.xctest */;
			productType = "com.apple.product-type.bundle.unit-test";
		}};
/* End PBXNativeTarget section */

/* Begin PBXProject section */
		{project_id} /* Project object */ = {{
			isa = PBXProject;
			attributes = {{
				BuildIndependentTargetsInParallel = 1;
				LastSwiftUpdateCheck = 1500;
				LastUpgradeCheck = 1500;
				TargetAttributes = {{
					{app_target} = {{
						CreatedOnToolsVersion = 15.0;
						ProvisioningStyle = Automatic;
					}};
					{test_target} = {{
						CreatedOnToolsVersion = 15.0;
						ProvisioningStyle = Automatic;
						TestTargetID = {app_target};
					}};
				}};
			}};
			buildConfigurationList = {project_config_list} /* Build configuration list for PBXProject "Motocam" */;
			compatibilityVersion = "Xcode 14.0";
			developmentRegion = zh-Hans;
			hasScannedForEncodings = 0;
			knownRegions = (
				en,
				"zh-Hans",
				Base,
			);
			mainGroup = {group_main};
			productRefGroup = {group_products} /* Products */;
			projectDirPath = "";
			projectRoot = "";
			targets = (
				{app_target} /* Motocam */,
				{test_target} /* MotocamTests */,
			);
		}};
/* End PBXProject section */

/* Begin PBXResourcesBuildPhase section */
		{app_resources_phase} /* Resources */ = {{
			isa = PBXResourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{resource_files}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};
/* End PBXResourcesBuildPhase section */

/* Begin PBXSourcesBuildPhase section */
		{app_sources_phase} /* Sources */ = {{
			isa = PBXSourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{app_source_files}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};
		{test_sources_phase} /* Sources */ = {{
			isa = PBXSourcesBuildPhase;
			buildActionMask = 2147483647;
			files = (
{test_source_files}
			);
			runOnlyForDeploymentPostprocessing = 0;
		}};
/* End PBXSourcesBuildPhase section */

/* Begin PBXTargetDependency section */
		{uid("dep:test")} /* PBXTargetDependency */ = {{
			isa = PBXTargetDependency;
			target = {app_target} /* Motocam */;
			targetProxy = {uid("proxy:test")} /* PBXContainerItemProxy */;
		}};
/* End PBXTargetDependency section */

/* Begin XCBuildConfiguration section */
		{debug_project} /* Debug */ = {{
			isa = XCBuildConfiguration;
			baseConfigurationReference = {debug_xcconfig} /* Debug.xcconfig */;
			buildSettings = {{
{project_common}
				ENABLE_TESTABILITY = YES;
				GCC_DYNAMIC_NO_PIC = NO;
				GCC_OPTIMIZATION_LEVEL = 0;
				GCC_PREPROCESSOR_DEFINITIONS = (
					"DEBUG=1",
					"$(inherited)",
				);
				MTL_ENABLE_DEBUG_INFO = INCLUDE_SOURCE;
				ONLY_ACTIVE_ARCH = YES;
				SWIFT_ACTIVE_COMPILATION_CONDITIONS = DEBUG;
				SWIFT_OPTIMIZATION_LEVEL = "-Onone";
			}};
			name = Debug;
		}};
		{release_project} /* Release */ = {{
			isa = XCBuildConfiguration;
			baseConfigurationReference = {release_xcconfig} /* Release.xcconfig */;
			buildSettings = {{
{project_common}
				ENABLE_NS_ASSERTIONS = NO;
				MTL_ENABLE_DEBUG_INFO = NO;
				SWIFT_COMPILATION_MODE = wholemodule;
				SWIFT_OPTIMIZATION_LEVEL = "-O";
				VALIDATE_PRODUCT = YES;
			}};
			name = Release;
		}};
		{debug_app} /* Debug */ = {{
			isa = XCBuildConfiguration;
			baseConfigurationReference = {debug_xcconfig} /* Debug.xcconfig */;
			buildSettings = {{
{common_target}
			}};
			name = Debug;
		}};
		{release_app} /* Release */ = {{
			isa = XCBuildConfiguration;
			baseConfigurationReference = {release_xcconfig} /* Release.xcconfig */;
			buildSettings = {{
{common_target}
			}};
			name = Release;
		}};
		{debug_test} /* Debug */ = {{
			isa = XCBuildConfiguration;
			baseConfigurationReference = {debug_xcconfig} /* Debug.xcconfig */;
			buildSettings = {{
{test_settings}
			}};
			name = Debug;
		}};
		{release_test} /* Release */ = {{
			isa = XCBuildConfiguration;
			baseConfigurationReference = {release_xcconfig} /* Release.xcconfig */;
			buildSettings = {{
{test_settings}
			}};
			name = Release;
		}};
/* End XCBuildConfiguration section */

/* Begin XCConfigurationList section */
		{project_config_list} /* Build configuration list for PBXProject "Motocam" */ = {{
			isa = XCConfigurationList;
			buildConfigurations = (
				{debug_project} /* Debug */,
				{release_project} /* Release */,
			);
			defaultConfigurationIsVisible = 0;
			defaultConfigurationName = Release;
		}};
		{app_config_list} /* Build configuration list for PBXNativeTarget "Motocam" */ = {{
			isa = XCConfigurationList;
			buildConfigurations = (
				{debug_app} /* Debug */,
				{release_app} /* Release */,
			);
			defaultConfigurationIsVisible = 0;
			defaultConfigurationName = Release;
		}};
		{test_config_list} /* Build configuration list for PBXNativeTarget "MotocamTests" */ = {{
			isa = XCConfigurationList;
			buildConfigurations = (
				{debug_test} /* Debug */,
				{release_test} /* Release */,
			);
			defaultConfigurationIsVisible = 0;
			defaultConfigurationName = Release;
		}};
/* End XCConfigurationList section */
	}};
	rootObject = {project_id} /* Project object */;
}}
"""

out = ROOT / "Motocam.xcodeproj"
out.mkdir(exist_ok=True)
(out / "project.pbxproj").write_text(pbx, encoding="utf-8")

scheme_dir = out / "xcshareddata" / "xcschemes"
scheme_dir.mkdir(parents=True, exist_ok=True)
scheme = f"""<?xml version="1.0" encoding="UTF-8"?>
<Scheme
   LastUpgradeVersion = "1500"
   version = "1.7">
   <BuildAction
      parallelizeBuildables = "YES"
      buildImplicitDependencies = "YES">
      <BuildActionEntries>
         <BuildActionEntry
            buildForTesting = "YES"
            buildForRunning = "YES"
            buildForProfiling = "YES"
            buildForArchiving = "YES"
            buildForAnalyzing = "YES">
            <BuildableReference
               BuildableIdentifier = "primary"
               BlueprintIdentifier = "{app_target}"
               BuildableName = "Motocam.app"
               BlueprintName = "Motocam"
               ReferencedContainer = "container:Motocam.xcodeproj">
            </BuildableReference>
         </BuildActionEntry>
      </BuildActionEntries>
   </BuildAction>
   <TestAction
      buildConfiguration = "Debug"
      selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB"
      selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB"
      shouldUseLaunchSchemeArgsEnv = "YES">
      <Testables>
         <TestableReference
            skipped = "NO">
            <BuildableReference
               BuildableIdentifier = "primary"
               BlueprintIdentifier = "{test_target}"
               BuildableName = "MotocamTests.xctest"
               BlueprintName = "MotocamTests"
               ReferencedContainer = "container:Motocam.xcodeproj">
            </BuildableReference>
         </TestableReference>
      </Testables>
   </TestAction>
   <LaunchAction
      buildConfiguration = "Debug"
      selectedDebuggerIdentifier = "Xcode.DebuggerFoundation.Debugger.LLDB"
      selectedLauncherIdentifier = "Xcode.DebuggerFoundation.Launcher.LLDB"
      launchStyle = "0"
      useCustomWorkingDirectory = "NO"
      ignoresPersistentStateOnLaunch = "NO"
      debugDocumentVersioning = "YES"
      debugServiceExtension = "internal"
      allowLocationSimulation = "YES">
      <BuildableProductRunnable
         runnableDebuggingMode = "0">
         <BuildableReference
            BuildableIdentifier = "primary"
            BlueprintIdentifier = "{app_target}"
            BuildableName = "Motocam.app"
            BlueprintName = "Motocam"
            ReferencedContainer = "container:Motocam.xcodeproj">
         </BuildableReference>
      </BuildableProductRunnable>
   </LaunchAction>
   <ProfileAction
      buildConfiguration = "Release"
      shouldUseLaunchSchemeArgsEnv = "YES"
      savedToolIdentifier = ""
      useCustomWorkingDirectory = "NO"
      debugDocumentVersioning = "YES">
      <BuildableProductRunnable
         runnableDebuggingMode = "0">
         <BuildableReference
            BuildableIdentifier = "primary"
            BlueprintIdentifier = "{app_target}"
            BuildableName = "Motocam.app"
            BlueprintName = "Motocam"
            ReferencedContainer = "container:Motocam.xcodeproj">
         </BuildableReference>
      </BuildableProductRunnable>
   </ProfileAction>
   <AnalyzeAction
      buildConfiguration = "Debug">
   </AnalyzeAction>
   <ArchiveAction
      buildConfiguration = "Release"
      revealArchiveInOrganizer = "YES">
   </ArchiveAction>
</Scheme>
"""
(scheme_dir / "Motocam.xcscheme").write_text(scheme, encoding="utf-8")
print(f"wrote {out} with {len(app_sources)} app sources and {len(test_sources)} tests")
