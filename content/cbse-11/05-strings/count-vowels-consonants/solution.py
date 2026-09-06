s = input()
vowels = 0
consonants = 0
upper = 0
lower = 0
for ch in s:
    if ch.isalpha():
        if ch.lower() in 'aeiou':
            vowels += 1
        else:
            consonants += 1
        if ch.isupper():
            upper += 1
        else:
            lower += 1
print('Vowels:', vowels)
print('Consonants:', consonants)
print('Uppercase:', upper)
print('Lowercase:', lower)
