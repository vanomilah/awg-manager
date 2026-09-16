import re

with open('internal/mihomo/types.go', 'r', encoding='utf-8') as f:
    text = f.read()

# Make sure imports contain crypto/sha256 and encoding/hex
if 'crypto/sha256' not in text:
    text = text.replace('\"context\"', '\"context\"\n\t\"crypto/sha256\"\n\t\"encoding/hex\"\n\t\"encoding/json\"')

if 'func (r BridgeRef) Digest() string' not in text:
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
