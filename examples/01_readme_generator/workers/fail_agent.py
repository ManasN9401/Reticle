import sys

def main():
    sys.stderr.write("panic: index out of range\n")
    sys.exit(1)

if __name__ == "__main__":
    main()
