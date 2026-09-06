n = int(input())
digits = len(str(n))
total = 0
temp = n
while temp > 0:
    d = temp % 10
    total += d ** digits
    temp //= 10
if total == n:
    print('Armstrong')
else:
    print('Not Armstrong')
