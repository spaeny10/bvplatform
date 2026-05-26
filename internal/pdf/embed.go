package pdf

import _ "embed"

// fontRegular holds Liberation Sans Regular TTF bytes, embedded at build time.
// The TTF file is placed into fonts/ by the Dockerfile build stage.
//
//go:embed fonts/LiberationSans-Regular.ttf
var fontRegular []byte

// fontBold holds Liberation Sans Bold TTF bytes, embedded at build time.
//
//go:embed fonts/LiberationSans-Bold.ttf
var fontBold []byte
