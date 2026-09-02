# @sightmap/sightkick

The **sightkick** CLI: compile a `webmcp.tools.yaml` + a sightmap `.sightmap/`
corpus into WebMCP tool IR, and install the sightkick agent skills.

Ships as a native binary (no Go toolchain required). `npm install` pulls in only
the `@sightmap/sightkick-<os>-<arch>` package matching your platform; the
`sightkick` launcher execs it.

```sh
npx @sightmap/sightkick build <corpus-dir> -o tools.ir.json
npx @sightmap/sightkick skills install         # -> ~/.agents/skills
```

See <https://github.com/sightmap/sightkick>.
