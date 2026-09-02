package yaml

import "testing"

func BenchmarkSerializeValue(b *testing.B) {
	strVal := "hello_world_secret"
	intVal := 123456
	boolVal := true
	sliceVal := []any{"item1", "item2", "item3"}
	mapVal := map[string]any{
		"host": "localhost",
		"port": 8080,
		"tls":  true,
	}

	b.Run("StringScalar", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeValue(strVal)
		}
	})

	b.Run("IntScalar", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeValue(intVal)
		}
	})

	b.Run("BoolScalar", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeValue(boolVal)
		}
	})

	b.Run("SliceValue", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeValue(sliceVal)
		}
	})

	b.Run("MapValue", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeValue(mapVal)
		}
	})
}
