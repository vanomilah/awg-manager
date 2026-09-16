import re

with open('internal/mihomo/types.go', 'r', encoding='utf-8') as f:
    text = f.read()

text = text.replace('\"context\"', '\"context\"\n\t\"crypto/sha256\"\n\t\"encoding/hex\"\n\t\"encoding/json\"')

idx = text.find('type BridgeRef struct')
end_idx = text.find('}', idx) + 1

digest_func = '''
func (r BridgeRef) Digest() string {
\tb, _ := json.Marshal(r)
\th := sha256.Sum256(b)
\treturn hex.EncodeToString(h[:])
}
'''
text = text[:end_idx] + '\n' + digest_func + text[end_idx:]

with open('internal/mihomo/types.go', 'w', encoding='utf-8') as f:
    f.write(text)
