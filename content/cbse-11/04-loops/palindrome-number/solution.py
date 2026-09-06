n = int(input())
rev = 0
temp = n
while temp > 0:
    rev = rev * 10 + temp % 10
    temp //= 10
if rev == n:
    print('Palindrome')
else:
    print('Not palindrome')
