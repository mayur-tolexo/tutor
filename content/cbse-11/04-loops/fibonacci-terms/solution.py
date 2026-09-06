n = int(input())
a, b = 0, 1
terms = []
for i in range(n):
    terms.append(str(a))
    a, b = b, a + b
print(' '.join(terms))
