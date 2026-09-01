lines = open('review-findings.md').read().split('\n')
assert lines[8] == '## Risk scoring', repr(lines[8])
assert lines[10].startswith('### 3.'), lines[10]
assert lines[22].startswith('## Applied statistics'), lines[22]
open('review-findings.md', 'w').write('\n'.join(lines[:8] + lines[22:]))
