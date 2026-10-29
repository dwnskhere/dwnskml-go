# Smart Caption in AZML-Go

Smart Caption is integrated with the existing custom leech caption setting. It is generated from the structured result produced by Smart Auto Rename and does not trigger another provider lookup or media probe.

## Default behavior

When the user has selected `Rename Method = smart` and no custom caption template is configured, AZML-Go generates a compact V2-style media caption, then preserves the incoming release name on a separate line when it differs:

```text
Example.Show.S02E05.Episode.Title.1080p.WEB-DL.HEVC.mkv

OG FileName: Example.Show.S02E05.1080p.WEB-DL.x265-GROUP.mkv
```

The exact fields depend on the filename parser, `ffprobe`, and any sufficiently confident canonical metadata match. Missing information is omitted rather than fabricated.

## Existing settings remain supported

Use the existing `/usersetting` → Leech → Caption menu, or the current caption-setting command, to set a caption template. Smart-only tags are populated in Smart rename mode; existing fields such as `{filename}`, `{size}`, `{duration}`, `{quality}`, `{languages}`, and `{subtitles}` remain supported by the existing non-Smart template path.

## Smart caption placeholders

| Placeholder | Meaning |
|---|---|
| `{filename}` | Actual physical filename (backward-compatible meaning) |
| `{physical_filename}` | Actual physical filename on disk |
| `{orig_filename}` / `{original_filename}` / `{prefilename}` | Original incoming filename, retained before renaming |
| `{smart_filename}` | Normalized filename proposed by the Smart Auto Rename builder |
| `{smartcaption}` / `{smartcaptionv2}` | Concise, dot-separated Smart Caption |
| `{smartcaptionv1}` | Smart Caption with per-track audio details when available |
| `{show_name}` / `{canonical_title}` | Canonical title selected by the metadata resolver, or parsed title fallback |
| `{season}` | Season number padded to two digits |
| `{episode}` | Episode number or episode range |
| `{episode_start}` / `{episode_end}` | Start and end episode numbers |
| `{title}` / `{episode_title}` | Episode title, when available |
| `{year}` | Parsed or canonical release year |
| `{quality}` | Resolution, preferably from actual media dimensions |
| `{source}` / `{platform}` | Release source and platform hints |
| `{codec}` / `{video_codec}` / `{encoder}` / `{release_group}` | Video codec and release/encoder hints |
| `{audio_summary}` / `{languages}` | Audio summary, based on stream metadata when available |
| `{subtitle}` / `{subtitles}` / `{subtitle_summary}` | Subtitle language summary |
| `{hdr}` / `{bit_depth}` | HDR and bit-depth metadata when available |
| `{split_suffix}` | Detected multipart suffix |
| `{size}` / `{duration}` / `{mime_type}` | File information; unavailable values are empty or a generic MIME type |
| `{provider}` / `{provider_id}` / `{match_confidence}` | Provider-match audit data |
| `{md5_hash}` | File hash. Calculated only when this placeholder is explicitly used, because hashing reads the entire file |

Example template:

```text
<b>{smartcaptionv2}</b>
OG FileName: <code>{orig_filename}</code>
Size: {size} | Duration: {duration}
Audio: {audio_summary} | Subs: {subtitle}
```

Dynamic values are HTML-escaped before insertion so incoming filenames or provider metadata cannot inject Telegram formatting. Template markup itself can contain Telegram-supported HTML tags.

## V1 and V2 differences

- **V1** includes detailed audio-track information when `ffprobe` reports it, such as language, codec, channel layout, and bitrate.
- **V2** uses the compact audio summary and is the default variant.

Both variants are deterministically rendered from metadata already in the rename result. They are captions, not separate physical file names. `{physical_filename}` always reports the actual name on disk, while `{smart_filename}` reports the proposed normalized name.

## Fallbacks and limitations

- Smart tags are intended for Smart rename mode; the standard template and regex modes remain unchanged.
- If metadata is missing, related values render empty. The caption does not invent canonical titles or episode titles.
- `{md5_hash}` can add substantial I/O for large files and should only be used when necessary.
- A split operation may upload multiple parts with a part header; the original incoming filename remains attached to the Smart Caption generated before splitting. Per-part reconstruction beyond the split metadata AZML-Go has retained should be validated against the configured splitter.
- Telegram captions have a length limit; the existing uploader truncation path still applies to the final rendered caption.
