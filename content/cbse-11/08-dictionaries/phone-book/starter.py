n = int(input())
book = {}
for i in range(n):
    name, number = input().split()
    book[name] = number
query = input()
