package semanticvector_test

import (
	"os"
	"strings"
	"testing"
)

const canonicalMITLicense = `MIT License

Copyright (c) 2026 jaresty

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

func TestLicenseAndReadmeStateCanonicalMIT(t *testing.T) {
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	if string(license) != canonicalMITLicense {
		t.Fatal("LICENSE differs from canonical MIT text for Copyright (c) 2026 jaresty")
	}

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "Licensed under the [MIT License](LICENSE). Copyright (c) 2026 jaresty.") {
		t.Error("README does not state the repository MIT license and copyright")
	}
}
