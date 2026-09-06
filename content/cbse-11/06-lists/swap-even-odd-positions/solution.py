n = int(input())
nums = list(map(int, input().split()))
for i in range(0, n - 1, 2):
    nums[i], nums[i + 1] = nums[i + 1], nums[i]
print(' '.join(map(str, nums)))
