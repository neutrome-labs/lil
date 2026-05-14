# Binary Encoding And Assembly

Programs can be serialized to a compact binary format for storage or wire
transfer.

## Wire Layout

```text
+----------------+---------+--------------------------------------+--------------+
| Magic (4B)     | Ver (1B)| Side-Buffers                         | Instructions |
| "AIL\x00"      | 0x01    | [count][len0][data0][len1][data1]... | [op][args]...|
+----------------+---------+--------------------------------------+--------------+
```

## Argument Types

| Type | Encoding |
|---|---|
| - | No argument, opcode only |
| String | 4-byte little-endian length prefix + UTF-8 bytes |
| Float | 8-byte IEEE 754 double, little-endian |
| Int | 4-byte little-endian signed integer |
| JSON | 4-byte little-endian length prefix + raw JSON bytes |
| RefID | 4-byte little-endian buffer index |
| Key,Val | Two length-prefixed strings back-to-back |
| Key,JSON | Length-prefixed key string + length-prefixed JSON |

## Encode / Decode

```go
var buf bytes.Buffer
err := prog.Encode(&buf)

prog, err := ail.Decode(&buf)
```

## Binary Layout Example

`{"role": "user", "content": "Hello"}` in AIL binary:

```text
10                              ; MSG_START
13                              ; ROLE_USR
20 05 00 00 00 48 65 6C 6C 6F  ; TXT_CHUNK len=5 "Hello"
11                              ; MSG_END
```

## Assembly Notation

`Program.Disasm()` produces a human-readable assembly listing with automatic
indentation inside block opcodes:

```asm
SET_MODEL gpt-4o
SET_TEMP 0.1000
MSG_START
  ROLE_SYS
  TXT_CHUNK Be brief.
MSG_END
MSG_START
  ROLE_USR
  TXT_CHUNK Hello
MSG_END
SET_STREAM
DEF_START
  DEF_NAME get_weather
  DEF_DESC Get current weather for a location
  DEF_SCHEMA {"type":"object","properties":{"location":{"type":"string"}}}
DEF_END
```

Comments prefixed with `;` can appear in assembly text and are silently ignored
by the parser.

