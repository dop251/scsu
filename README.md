SCSU
====

A Standard Compression Scheme for Unicode implementation in Go.

[![GoDoc](https://godoc.org/github.com/dop251/scsu?status.svg)](https://godoc.org/github.com/dop251/scsu)

This in an implementation of SCSU as described in https://www.unicode.org/reports/tr6/tr6-4.html

Although UTF-8 is now the most commonly used and recommended encoding, in some cases the use of SCSU can
be beneficial. For example when storing or transmitting short alphabetical texts (Arabic, Hebrew, Russian, etc.)
where general-purpose compression algorithms are inefficient, but SCSU provides nearly 50% compression ratio
over UTF-8.

The code is based on the sample Java implementation found at ftp://ftp.unicode.org/Public/PROGRAMS/SCSU/ however the
encoding algorithm has been slightly modified as the implementation above contains a few bugs.

The code has been fuzz-tested using https://github.com/dvyukov/go-fuzz to ensure that random input neither crashes the
Encoder nor the Decoder, and if it happens to be a valid UTF-8, an Encode/Decode cycle produces identical output.

Usage Scenarios.
-----

Encode a string into a []byte:

```go
b, err := scsu.Encode(s, nil) // the second argument can be an existing slice which will be appended
```

Decode a []byte into a string:

```go
s, err := scsu.Decode(b)
```

Use an Encoder:
```go
encoder := scsu.NewEncoder(writer)
n, err := encoder.WriteString(s)
n, err = encoder.WriteRune(r)
n, err = encoder.Encode(runeSource)
```

Use a Decoder:
```go
decoder := scsu.NewDecoder(byteReader)
s, err := decoder.ReadString() // read the entire string
r, size, err := decoder.ReadRune() // or a single rune
```
