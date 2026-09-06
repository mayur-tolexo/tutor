n = int(input())
nums = list(map(int, input().split()))
largest = nums[0]
smallest = nums[0]
for x in nums:
    if x > largest:
        largest = x
    if x < smallest:
        smallest = x
print('Largest:', largest)
print('Smallest:', smallest)
