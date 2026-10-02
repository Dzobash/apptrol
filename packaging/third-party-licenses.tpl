Third-party licenses
====================

Apptrol's binary contains the following libraries. Their copyright notices and
license texts are reproduced below, as their licenses require. Apptrol's own
license is in LICENSE. This file is generated on every release
(make third-party-licenses, with go-licenses).
{{ range . }}
--------------------------------------------------------------------------------
{{ .Name }} {{ .Version }}
License: {{ .LicenseName }}
Source:  {{ .LicenseURL }}
--------------------------------------------------------------------------------

{{ .LicenseText }}
{{ end }}
