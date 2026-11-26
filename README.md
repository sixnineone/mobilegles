# mobilegles

The renderer behind [MeUI](https://github.com/sixnineone/meui): an SDL3 + OpenGL batch
renderer for shapes, text and images, with a software fallback for headless rendering.

It is a pure-Go module. Calls into SDL3 and OpenGL go through purego, so the desktop
build works with `CGO_ENABLED=0`; cgo is only needed for the mobile cross-builds.

```go
import "github.com/sixnineone/mobilegles"
```

`resources/fonts/` holds the embedded Roboto faces (Apache 2.0). The shaders are in
`shaders/`. `cmd/` contains the renderer's own probes and a demo.

## Build

```sh
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
```

## Licence

MIT — see `LICENSE`. Roboto is Apache 2.0 (see `LICENSES/Apache-2.0.txt`); other
dependency notices are in `LICENSES/`.
