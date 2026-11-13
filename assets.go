package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

const assetOutputDirectory = "assets"

// A catalogue is private to a completed build and immutable after publication.
type AssetCatalogue struct {
	entries   map[string]assetEntry
	scripts   map[string]bool
	outputs   map[string]assetDigests
	originals map[string]struct{}
}

// Payloads belong only to the build/publication operation, never the active theme.
type assetBuild struct {
	catalogue *AssetCatalogue
	outputs   map[string][]byte
	originals map[string][]byte
}

type assetDigests struct {
	sha256 string
	sha384 string
	sha512 string
}

type assetEntry struct {
	output string
	css    string
}

type assetMetadata struct {
	Inputs  map[string]assetInput  `json:"inputs"`
	Outputs map[string]assetOutput `json:"outputs"`
}

type assetInput struct {
	Format string `json:"format"`
}

type assetOutput struct {
	EntryPoint string        `json:"entryPoint"`
	CSSBundle  string        `json:"cssBundle"`
	Imports    []assetImport `json:"imports"`
}

type assetImport struct {
	Path string `json:"path"`
}

func BuildThemeAssets(source fs.FS, options ConfigAssets) (*assetBuild, error) {
	err := options.Validate()
	if err != nil {
		return nil, err
	}

	files, err := readThemeAssets(source)
	if err != nil {
		return nil, err
	}

	catalogue := &AssetCatalogue{
		entries:   make(map[string]assetEntry, len(files)),
		scripts:   make(map[string]bool, len(files)),
		outputs:   make(map[string]assetDigests, len(files)),
		originals: make(map[string]struct{}, len(files)),
	}
	build := &assetBuild{
		catalogue: catalogue,
		outputs:   make(map[string][]byte, len(files)),
		originals: make(map[string][]byte, len(files)),
	}

	for _, name := range options.Entrypoints {
		if _, exists := files[name]; !exists {
			return nil, fmt.Errorf("theme entrypoint %q does not exist", name)
		}
	}

	for name, contents := range files {
		extension := path.Ext(name)
		if extension == ".js" || extension == ".mjs" {
			catalogue.scripts[name] = true

			if options.Compile {
				continue
			}
		} else if extension != ".css" {
			original := path.Join("assets", name)
			build.originals[original] = contents
			catalogue.originals[original] = struct{}{}

			continue
		}

		err = buildAsset(build, files, name, options)
		if err != nil {
			return nil, err
		}
	}

	for _, name := range options.Entrypoints {
		err = buildAsset(build, files, name, options)
		if err != nil {
			return nil, err
		}
	}

	for name, contents := range build.originals {
		if output, exists := build.outputs[name]; exists && !bytes.Equal(contents, output) {
			return nil, fmt.Errorf("theme resource %q conflicts with a generated output; rename the source resource", name)
		}
	}

	return build, nil
}

func readThemeAssets(source fs.FS) (map[string][]byte, error) {
	files := make(map[string][]byte)

	if source == nil {
		return files, nil
	}

	err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if name == "." {
			return nil
		}

		if !validAssetPath(name) {
			return fmt.Errorf("invalid theme asset path %q", name)
		}

		if entry.IsDir() {
			return nil
		}

		if !entry.Type().IsRegular() {
			return fmt.Errorf("theme asset %q must be a regular file (symlinks are not supported)", name)
		}

		contents, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}

		files[name] = contents

		return nil
	})

	return files, err
}

func buildAsset(build *assetBuild, files map[string][]byte, name string, settings ConfigAssets) error {
	isCSS := path.Ext(name) == ".css"
	compile := settings.Compile || isCSS
	classic := !compile && path.Ext(name) == ".js"
	catalogue := build.catalogue

	workingDirectory, err := filepath.Abs(".")
	if err != nil {
		return err
	}

	options := api.BuildOptions{
		AbsWorkingDir:       workingDirectory,
		Outdir:              assetOutputDirectory,
		EntryPointsAdvanced: []api.EntryPoint{{InputPath: name, OutputPath: strings.TrimSuffix(name, path.Ext(name))}},
		EntryNames:          "[dir]/[name]-[hash]",
		AssetNames:          "[name]-[hash]",
		PublicPath:          "/" + assetOutputDirectory,
		Bundle:              true,
		Platform:            api.PlatformBrowser,
		Format:              api.FormatIIFE,
		Target:              api.ES2020,
		TreeShaking:         api.TreeShakingFalse,
		Metafile:            true,
		Write:               false,
		LogLevel:            api.LogLevelSilent,
		MinifyWhitespace:    settings.Minify,
		MinifyIdentifiers:   settings.Minify,
		MinifySyntax:        settings.Minify,
		LogOverride: map[string]api.LogLevel{
			"empty-glob":                   api.LogLevelError,
			"empty-import-meta":            api.LogLevelError,
			"indirect-require":             api.LogLevelError,
			"unsupported-require-call":     api.LogLevelError,
			"unsupported-dynamic-import":   api.LogLevelError,
			"require-resolve-not-external": api.LogLevelError,
		},
		Plugins: []api.Plugin{themeAssetPlugin(files, compile, classic)},
	}

	if classic {
		// Classic scripts may share globals or contain guarded UMD branches.
		// Probe browser branches without the CommonJS bindings bundling invents.
		// None of these definitions apply to the published classic script.
		options.Define = map[string]string{"module": "undefined", "exports": "undefined"}
		options.LogOverride["indirect-require"] = api.LogLevelSilent
		options.LogOverride["unsupported-require-call"] = api.LogLevelSilent
		options.LogOverride["require-resolve-not-external"] = api.LogLevelSilent
	}

	result := api.Build(options)

	err = assetBuildError(name, result)
	if err != nil {
		return err
	}

	if classic {
		err = validateBrowserScript(name, files[name], true)
		if err != nil {
			return err
		}
	}

	var metadata assetMetadata

	err = json.Unmarshal([]byte(result.Metafile), &metadata)
	if err != nil {
		return err
	}

	if !compile {
		input := metadata.Inputs["theme:"+name]
		if input.Format == "cjs" && !classic || input.Format == "esm" && classic {
			return fmt.Errorf("theme script %q uses module syntax: enable theme.assets.compile and declare its entrypoint (or use .mjs for a self-contained ES module)", name)
		}
	}

	if !compile {
		options.Bundle = false
		options.Format = api.FormatIIFE
		options.Define = nil

		if path.Ext(name) == ".js" {
			options.Format = api.FormatDefault
		}

		result = api.Build(options)

		err = assetBuildError(name, result)
		if err != nil {
			return err
		}

		metadata = assetMetadata{}

		err = json.Unmarshal([]byte(result.Metafile), &metadata)
		if err != nil {
			return err
		}
	}

	for _, output := range result.OutputFiles {
		if !classic && strings.HasSuffix(output.Path, ".js") {
			err = validateBrowserScript(name, output.Contents, false)
			if err != nil {
				return err
			}
		}

		relative, err := filepath.Rel(workingDirectory, output.Path)
		if err != nil {
			return err
		}

		outputName := filepath.ToSlash(relative)
		if previous, exists := build.outputs[outputName]; exists {
			if !bytes.Equal(previous, output.Contents) {
				return fmt.Errorf("theme output %q conflicts with another generated asset", outputName)
			}

			continue
		}

		build.outputs[outputName] = output.Contents
		catalogue.outputs[outputName] = digestAsset(output.Contents)
	}

	for outputName, output := range metadata.Outputs {
		if output.EntryPoint != "theme:"+name || isCSS != strings.HasSuffix(outputName, ".css") {
			continue
		}

		catalogue.entries[name] = assetEntry{output: outputName, css: output.CSSBundle}
	}

	if _, exists := catalogue.entries[name]; !exists {
		return fmt.Errorf("esbuild did not produce an entry for %q", name)
	}

	return nil
}

// Use esbuild's symbol resolution to detect surviving global require references,
// including computed property access and helpers that esbuild cannot bundle.
func validateBrowserScript(name string, contents []byte, classic bool) error {
	options := api.BuildOptions{
		Stdin:       &api.StdinOptions{Contents: string(contents), Sourcefile: name},
		Bundle:      true,
		Write:       false,
		Metafile:    true,
		LogLevel:    api.LogLevelSilent,
		TreeShaking: api.TreeShakingTrue,
		Define:      map[string]string{"require": "__mdgo_browser_require__"},
		Inject:      []string{"mdgo:require-probe"},
		Plugins: []api.Plugin{{
			Name: "browser-require-probe",
			Setup: func(build api.PluginBuild) {
				build.OnResolve(api.OnResolveOptions{Filter: "^mdgo:"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					return api.OnResolveResult{Path: args.Path, Namespace: "probe", External: args.Path == "mdgo:browser-require", SideEffects: api.SideEffectsFalse}, nil
				})

				build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "probe"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
					return api.OnLoadResult{Contents: new(`export { default as __mdgo_browser_require__ } from "mdgo:browser-require";`), Loader: api.LoaderJS}, nil
				})
			},
		}},
	}

	if classic {
		// Substitute browser bindings before bundling can invent CommonJS locals.
		browser := api.Transform(string(contents), api.TransformOptions{
			Sourcefile:   name,
			Define:       map[string]string{"module": "undefined", "exports": "undefined"},
			MinifySyntax: true,
			TreeShaking:  api.TreeShakingFalse,
			LogLevel:     api.LogLevelSilent,
		})

		err := assetBuildError(name, api.BuildResult{Errors: browser.Errors})
		if err != nil {
			return err
		}

		options.Stdin.Contents = string(browser.Code)

		// Classic globals can be called by another script, so retain unused declarations.
		options.TreeShaking = api.TreeShakingFalse
	}

	result := api.Build(options)

	err := assetBuildError(name, result)
	if err != nil {
		return err
	}

	var metadata assetMetadata

	err = json.Unmarshal([]byte(result.Metafile), &metadata)
	if err != nil {
		return err
	}

	for _, output := range metadata.Outputs {
		for _, imported := range output.Imports {
			if imported.Path == "mdgo:browser-require" {
				if classic {
					return fmt.Errorf("theme script %q retains an unresolved CommonJS dependency: enable theme.assets.compile and declare entrypoints", name)
				}

				return fmt.Errorf("theme script %q retains a runtime require reference (including require.resolve): use static relative imports or static require calls that esbuild can bundle", name)
			}
		}
	}

	return nil
}

func themeAssetPlugin(files map[string][]byte, compile, classic bool) api.Plugin {
	return api.Plugin{
		Name: "theme-assets",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if classic && (args.Kind == api.ResolveJSRequireCall || args.Kind == api.ResolveJSRequireResolve) {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}

				if args.Kind == api.ResolveJSRequireResolve {
					return api.OnResolveResult{}, fmt.Errorf("%q: require.resolve is unsupported in browser assets; use a static import", args.Importer)
				}

				name := args.Path
				suffix := ""

				isCSS := args.Kind == api.ResolveCSSImportRule || args.Kind == api.ResolveCSSURLToken
				if isCSS && (strings.HasPrefix(name, "https://") || strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "//") || strings.HasPrefix(name, "data:") || strings.HasPrefix(name, "#")) {
					return api.OnResolveResult{Path: name, External: true}, nil
				}

				if args.Kind != api.ResolveEntryPoint {
					if !compile {
						return api.OnResolveResult{}, fmt.Errorf("%q imports %q: enable theme.assets.compile and declare entrypoints", args.Importer, name)
					}

					if !isCSS && !strings.HasPrefix(name, "./") && !strings.HasPrefix(name, "../") {
						return api.OnResolveResult{}, fmt.Errorf("%q: import %q must be a relative theme asset; packages and external JavaScript are unsupported", args.Importer, name)
					}

					index := strings.IndexAny(name, "?#")
					if index >= 0 {
						suffix = name[index:]
						name = name[:index]
					}

					if strings.HasPrefix(name, "/assets/") && isCSS {
						name = strings.TrimPrefix(name, "/assets/")
					} else if strings.HasPrefix(name, "/") {
						return api.OnResolveResult{}, fmt.Errorf("asset reference %q is outside the theme assets directory", args.Path)
					} else {
						name = path.Join(path.Dir(args.Importer), name)
					}
				}

				if !validAssetPath(name) {
					return api.OnResolveResult{}, fmt.Errorf("invalid or escaping theme asset reference %q from %q", args.Path, args.Importer)
				}

				if _, exists := files[name]; !exists {
					return api.OnResolveResult{}, fmt.Errorf("theme asset %q referenced by %q does not exist; use an explicit filename relative to the importer", name, args.Importer)
				}

				return api.OnResolveResult{Path: name, Namespace: "theme", Suffix: suffix}, nil
			})

			build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "theme"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				loader, err := themeAssetLoader(path.Ext(args.Path))
				if err != nil {
					return api.OnLoadResult{}, err
				}

				contents := string(files[args.Path])

				return api.OnLoadResult{Contents: new(contents), Loader: loader}, nil
			})

			// Computed import globs bypass OnResolve. Never allow their matches
			// to fall through to esbuild's native filesystem loader.
			build.OnLoad(api.OnLoadOptions{Filter: ".*"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				return api.OnLoadResult{}, fmt.Errorf("cannot load %q outside the theme assets filesystem; replace computed imports with explicit relative filenames", args.Path)
			})
		},
	}
}

func themeAssetLoader(extension string) (api.Loader, error) {
	switch extension {
	case ".js", ".mjs":
		return api.LoaderJS, nil
	case ".css":
		return api.LoaderCSS, nil
	case ".json":
		return api.LoaderJSON, nil
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".avif", ".ico", ".woff", ".woff2", ".ttf", ".otf", ".eot":
		return api.LoaderFile, nil
	default:
		return api.LoaderNone, fmt.Errorf("unsupported imported asset type %q; use .js/.mjs, CSS, JSON, or a supported image/font", extension)
	}
}

func assetBuildError(name string, result api.BuildResult) error {
	if len(result.Errors) == 0 {
		return nil
	}

	messages := api.FormatMessages(result.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage, Color: false})

	return fmt.Errorf("build theme asset %q: %s", name, strings.Join(messages, "\n"))
}

func digestAsset(contents []byte) assetDigests {
	sha256Sum := sha256.Sum256(contents)
	sha384Sum := sha512.Sum384(contents)
	sha512Sum := sha512.Sum512(contents)

	return assetDigests{
		sha256: "sha256-" + base64.StdEncoding.EncodeToString(sha256Sum[:]),
		sha384: "sha384-" + base64.StdEncoding.EncodeToString(sha384Sum[:]),
		sha512: "sha512-" + base64.StdEncoding.EncodeToString(sha512Sum[:]),
	}
}
