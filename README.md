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
for key, raw := range result.Unknown {
    log.Printf("unexpected field %s: %s", key, raw)
}
if len(result.Missing) > 0 {
    log.Printf("missing fields: %v", result.Missing)
}
```

`result.Unknown` is a `map[string]json.RawMessage` — each unknown field's raw
JSON value is preserved for inspection or further decoding.

The target must be a non-nil pointer to a struct.

## Performance

The JSON input is parsed twice: once to identify unknown and missing keys,
then again to populate the struct. This means ~2x the memory usage and CPU
of a plain `json.Unmarshal`. Callers processing large payloads should bound
input size before calling `Unmarshal`.

## License

Apache 2.0
