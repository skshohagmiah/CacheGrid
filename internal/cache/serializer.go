package cache

import "github.com/vmihailenco/msgpack/v5"

// Serialize encodes a value to msgpack bytes.
// If the value is already []byte, it is returned as-is.
func Serialize(value interface{}) ([]byte, error) {
	if b, ok := value.([]byte); ok {
		return b, nil
	}
	return msgpack.Marshal(value)
}

// Deserialize decodes msgpack bytes into dest.
// If dest is *[]byte, the raw bytes are copied directly.
func Deserialize(data []byte, dest interface{}) error {
	if p, ok := dest.(*[]byte); ok {
		*p = make([]byte, len(data))
		copy(*p, data)
		return nil
	}
	return msgpack.Unmarshal(data, dest)
}
