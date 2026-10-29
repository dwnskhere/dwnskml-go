# Smart Auto Rename Port — Implementation Plan and Verification Notes

## Repository findings

- Existing AZML-Go rename and caption behavior is concentrated in `helper/ext/leech_utils.go` (`ExtractMetadataFromFilename`, `ApplyTemplateRename`, `applyRegexRemname`, `FormatFilename`).
- Standard leech flow processes files in `modules/leech.go`; yt-dlp flow processes one output in `modules/ytdl.go`. Both currently perform `os.Rename` without checking its error.
- `helper/ext/ffmpeg.go` already invokes `ffprobe`, but its current `MediaDetails` model does not include enough structured audio/subtitle stream flags for Smart Rename.
- Per-user settings are stored in `types.UserSettings` and persisted through `ext.DB.SaveUser`; `RenameMethod` currently cycles between `auto` and `regex`. `/ar` is handled in `modules/settings.go`.
- Global TMDb token support already exists as `TMDB_ACCESS_TOKEN`; no additional Python runtime should be introduced.
- Reference WZMLHK-X is a staged parser/probe/provider/builder/fitter system. Its `TVDBProvider` delegates to Cinemeta in this source snapshot, and its provider order does not prove that a direct TVDB API is used.

## Implementation approach

1. Add typed Smart Rename models, parser, media probe, metadata resolver, filename builder/fitter, and orchestration in cohesive `helper/ext/smart_*.go` files.
2. Keep template and regex helpers intact; add `smart` as an opt-in third `RenameMethod` value and preserve old defaults for existing records/deployments.
3. Implement optional TMDb and public Kitsu/Cinemeta lookups using bounded standard-library HTTP requests. External lookups remain optional, timeout-bounded, cached, and must fall back to parsed metadata. Avoid claiming direct IMDb/TVDB integration where there is no stable unauthenticated API in this codebase.
4. Probe streams with `ffprobe` through a context-bound process. A probe failure is non-fatal; filename evidence remains available.
5. Use a deterministic builder and a Unicode-safe configurable filename fitter (default 60 Unicode code points including extension, prefixes, suffixes and split markers).
6. Integrate through a shared smart preparation path used by leech and yt-dlp, skip explicit custom names, preserve the original filename for captions, and only update tracked paths after successful filesystem operations.
7. Add `/ar mode auto|regex|smart` and support a simple `/ar smart` shortcut while leaving existing `/ar <template>` and `/ar off` semantics intact. Update the settings menu's mode toggle and labels.
8. Add offline unit tests for parsing, fitting, stream parsing, provider scoring/fallbacks, filesystem collision behavior, and mode compatibility.

## Backward compatibility

- Existing `auto` and `regex` settings, `RemnameAuto`, `RemnameRegex`, prefixes, suffixes, captions and the current `FormatFilename` API remain available.
- The new feature defaults to not being selected: a legacy `RenameMethod="auto"` remains template mode. Smart Rename runs only when `Autorename=true`, `RenameMethod="smart"`, no explicit custom name is supplied, and `SMART_AUTORENAME_ENABLED` is enabled.
- Existing caption placeholders remain. Smart mode additionally supports `{orig_filename}`, `{smart_filename}`, `{show_name}`, `{season}`, `{episode}`, `{year}`, `{source}`, `{codec}`, `{audio_summary}`, `{subtitle}`, `{hdr}`, and `{bit_depth}`; unavailable values resolve to empty strings.

## Files created or modified

Created:

- `helper/ext/smart_autorename.go` — typed result/options, orchestration, safe fallback captions and caption placeholder expansion.
- `helper/ext/smart_filename_parser.go` — conservative parsing and confidence/evidence tracking.
- `helper/ext/smart_media_probe.go` — `ffprobe` JSON probing and stream metadata.
- `helper/ext/smart_metadata_resolver.go` — provider interface/adapters, candidate scoring, caching, request coalescing and bounded HTTP behavior.
- `helper/ext/smart_filename_builder.go` — media-specific naming, Unicode-safe fitting and no-overwrite filesystem handling.
- `helper/ext/smart_autorename_test.go` — offline unit tests.
- `docs/SMART_AUTORENAME.md` — configuration, commands, output conventions and limitations.

Modified:

- `config/config.go`, `config.env.sample` — Smart feature gate, lookup, provider priority, maximum filename length, and probe/lookup timeouts.
- `types/types.go` — documented existing `RenameMethod` values, including `smart` (no new database field required).
- `helper/ext/leech_utils.go` — keep `smart` mode out of the template path; legacy modes remain available.
- `modules/settings.go` — third rename mode, settings-menu cycle, `/ar mode ...` and `/ar smart` commands.
- `modules/leech.go`, `modules/ytdl.go` — shared Smart rename invocation, original-name preservation, path updates only after success, checked legacy renames and safer directory-file filtering.
- `README.md` — link to the Smart Auto Rename guide.

## Verification results

- `gofmt` was run on all changed Go files.
- The Smart Auto Rename files and tests were copied into an isolated test module with small local `config`/`types`/utility stubs so the new subsystem could be exercised without fetching AZML-Go's unrelated dependencies. In that harness, `GOTOOLCHAIN=local go test ./helper/ext`, `GOTOOLCHAIN=local go test -race ./helper/ext`, `GOTOOLCHAIN=local go test -cover ./helper/ext`, and `GOTOOLCHAIN=local go vet ./helper/ext` passed. Coverage in that isolated harness was 61.2% of Smart subsystem statements. It is not a substitute for compiling the full repository.
- Tests use only `httptest` providers and an injected mock for ffprobe; they do not require live provider endpoints or API keys.
- Full repository `go test ./...` could not start: `go.mod` declares Go 1.25.0, the environment has Go 1.23.2, and downloading the Go 1.25 toolchain failed because `proxy.golang.org` DNS/network access is unavailable.
- A temporary copy with the `go` directive lowered to 1.23.0 was also attempted with `GOPROXY=off`; it stopped at missing cached dependencies (`godotenv`, `gotd/td`, MongoDB driver, etc.). No claim is made that full-module tests or runtime/integration tests passed.

## Intentional deviations and known limitations

- Existing users remain on template mode by default. Smart is opt-in through the user's `RenameMethod`, while `SMART_AUTORENAME_ENABLED` is a global gate.
- TMDb, Kitsu and Cinemeta adapters are implemented. No direct IMDb adapter is included; the Python reference's `imdbio` dependency was not ported. The reference's `TVDBProvider` delegates to Cinemeta in the supplied source snapshot, so no direct TVDB API client is claimed.
- Metadata and stream probe failures are non-fatal. Filename identity with insufficient evidence is skipped rather than guessed. If no confident provider match is available, local parsed metadata is retained.
- Physical rename uses an atomic hard-link-then-unlink strategy to avoid overwriting an existing destination. Filesystems that disallow hard links cause a safe rename fallback (original path retained), rather than an unsafe overwriting `os.Rename` fallback.
- Full pipeline compilation and live upload validation remain necessary in an environment with Go 1.25 and the repository dependencies available.
