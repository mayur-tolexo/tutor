s = input()
freq = {}
for word in s.split():
    if word in freq:
        freq[word] += 1
    else:
        freq[word] = 1
for word in freq:
    print(word + ':', freq[word])
