# sw33tLie's net/http

This is a fork of the Go standard library's `net/http` package that removes several validation checks that can be restrictive when developing security tools and penetration testing utilities.

## Purpose

The standard `net/http` package enforces various RFC compliance checks that, while appropriate for production applications, can hinder security research and testing. This fork relaxes these restrictions to allow for more flexible HTTP request crafting.

## Patches

The following modifications have been made:

- **Unrestricted header values**: All characters are now allowed in header field names and values (including spaces, tabs and similar)
- **No header canonicalization**: HTTP header names are not canonicalized anymore (e.g. `x-test: asd` is not changed to `X-Test: asd`)
- **Default User-Agent**: The default user agent has been changed to a more common browser string (latest Chrome)

Note that all patches have this comment to easily spot them in the code:

```
// sw33tLie patch
```

## Usage

First, install the package in your project:

```bash
go get github.com/sw33tLie/http
```

Then, replace your `net/http` import with `github.com/sw33tLie/http`:

```go
import (
	http "github.com/sw33tLie/http"
)
```

You're good to go!
