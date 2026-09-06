n = int(input())
students = {}
for i in range(n):
    roll, name, marks = input().split()
    students[int(roll)] = (name, int(marks))
found = False
for roll in students:
    name, marks = students[roll]
    if marks > 75:
        print(name)
        found = True
if not found:
    print('None')
