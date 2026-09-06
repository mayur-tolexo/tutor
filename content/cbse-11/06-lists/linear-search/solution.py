n = int(input())
nums = list(map(int, input().split()))
target = int(input())
found = -1
for i in range(n):
    if nums[i] == target:
        found = i
        break
if found == -1:
    print('Not found')
else:
    print(found)
