# MM isolation wrapper

`mm-isolation` is a Streamable HTTP proxy for the deployed Qwen MM services.

## Deployment contract

- The backend signs `_meta` fields `user_id`, `conversation_id`, `request_id`, `call_id`, and `ts` with HMAC-SHA256.
- `tools/call` requires nonzero user and conversation IDs, non-empty request and call IDs, and a timestamp within five minutes. A process-local replay cache rejects the same signed call ID within that window while allowing multiple calls in one HTTP request.
- Virtual `/shared/...` and `/imports/...` paths are accepted only inside the signed `deeix-<user>-<conversation>` scope. Absolute values in path-like arguments are checked as well.
- Linux builds open inputs relative to the source root with `openat2`, using `RESOLVE_BENEATH`, `RESOLVE_NO_SYMLINKS`, and `RESOLVE_NO_MAGICLINKS`. Cross-scope paths, traversal, symlinks, non-regular files, and inputs over 64 MiB are rejected.
- Accepted inputs are copied to a hash-suffixed per-call lane with mode `0755`; files use mode `0644` so the separately namespaced upstream container can read its read-only staging mount. Rewritten upstream paths point into that lane. Files with the same basename do not overwrite one another.
- Each wrapper serializes `tools/call` requests to its upstream. It clears the mounted staging root before the call and after the upstream response, so the persistent upstream sees files from at most one signed call at a time.
- The pinned Qwen core commit has three caller-visible producer tools: `crop`, `draw_bbox`, and `save_view`. The core wrapper overrides caller-provided `output_path`/`output_dir` with a per-call staging lane. Other tools do not receive a writable output lane.
- Producer outputs are limited to 32 regular files, 20 MiB per file, and 64 MiB total. Symlinks, non-regular files, empty files, and limit violations reject the entire output set before publication.
- Published producer output is producer-owned: the wrapper does not remove successful files after backend attachment persistence. A context-controlled sweeper removes expired producer-formatted `mm-<lane>-<hash>` directories in valid scopes and root `.tmp-mm-<random>` crash directories only when `MM_OUTPUT_SHARED` is configured. Defaults are 24 hours retention and a one-hour sweep interval; source, scope, input, import, and user-created directories are never swept.
- MM upstream containers attach only to `mm-private`. The API upstream mounts staging read-only. The core upstream mounts only its per-call staging writable so the three producer tools can create files; neither upstream sees the complete shared/imports trees. Wrappers attach to `onepanel` and `mm-private`; source mounts are read-only, and only the core wrapper mounts the shared output root writable.
- Existing MCP server names and URLs remain stable: `qwen-mm-core` at port 8082 and `qwen-mm-omni-av` at port 8083.

Build and test with:

```sh
docker build -f tools/mm-isolation/Dockerfile -t deeix-mm-isolation:test .
docker run --rm -v "$PWD/tools/mm-isolation:/src" -w /src golang:1.23-alpine sh -c 'gofmt -w *.go && go test ./...'
```
