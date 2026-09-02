module sightkick/generator

go 1.23

require (
	github.com/sightmap/sightmap/go v0.28.0
	gopkg.in/yaml.v3 v3.0.1
)

// TEMPORARY: build against the local sightmap checkout so the build-time
// extractor verifier (verify.go) uses the offline text-parity extraction
// (rendered node text for role-less nodes). Revert to the plain v0.28.x pin
// once that lands in a sightmap release and bump the require above.
replace github.com/sightmap/sightmap/go => /Users/joel/src/fs/subtext/sightmap/go
