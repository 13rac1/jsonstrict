# jsonstrict

Go package that unmarshals JSON like `encoding/json`, but also reports unknown
and missing fields. Neither causes an error — callers decide what to do.

If you only need to reject unknown fields without knowing which ones,
use `json.Decoder.DisallowUnknownFields` from the standard library instead.

## Install

```
go get github.com/13rac1/jsonstrict
```

## Usage

```go
var config Config
result, err := jsonstrict.Unmarshal(data, &config)
if err != nil {
    return err
}
if len(result.Unknown) > 0 {
    log.Printf("unexpected fields: %v", result.Unknown)
}
if len(result.Missing) > 0 {
    log.Printf("missing fields: %v", result.Missing)
}
```

The target must be a non-nil pointer to a struct.

## License

Apache 2.0
