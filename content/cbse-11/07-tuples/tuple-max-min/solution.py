n = int(input())
t = tuple(map(int, input().split()))
largest = t[0]
smallest = t[0]
for x in t:
    if x > largest:
        largest = x
    if x < smallest:
        smallest = x
print('Largest:', largest)
print('Smallest:', smallest)
