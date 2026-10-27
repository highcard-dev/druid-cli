package registry

// TransferOptions controls whether a runtime release descriptor travels with a
// snapshot. Normal Scroll publication excludes manifest.json because it
// describes a local installation, not the artifact being published.
type TransferOptions struct {
	PreserveReleaseManifest bool
}
