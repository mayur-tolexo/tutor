n = int(input())
nums = list(map(int, input().split()))
total = 0
for x in nums:
    total += x
print('Sum:', total)
print('Average:', total / n)
