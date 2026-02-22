package shortcode

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func Encode(num int64) string {
	if num == 0 {
		return string(alphabet[0])
	}

	var result []byte
	base := int64(len(alphabet))

	for num > 0 {
		result = append([]byte{alphabet[num%base]}, result...)
		num /= base
	}

	return string(result)
}
