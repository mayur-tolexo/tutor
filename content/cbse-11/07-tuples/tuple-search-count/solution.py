n = int(input())
t = tuple(map(int, input().split()))
target = int(input())
count = 0
for x in t:
    if x == target:
        count += 1
if count > 0:
    print('Found: Yes')
else:
    print('Found: No')
print('Count:', count)
