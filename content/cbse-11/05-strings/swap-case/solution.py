s = input()
result = ''
for ch in s:
    if ch.isupper():
        result += ch.lower()
    elif ch.islower():
        result += ch.upper()
    else:
        result += ch
print(result)
