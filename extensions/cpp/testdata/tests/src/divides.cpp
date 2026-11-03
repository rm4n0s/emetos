#include <cstdlib>
#include <iostream>

static int divide(int a, int b) { return a / b; }

int main(int argc, char **argv) {
	int a = argc > 1 ? std::atoi(argv[1]) : 0;
	int b = argc > 2 ? std::atoi(argv[2]) : 1;
	int got = divide(a, b);
	std::cout << "divide(" << a << ", " << b << ") = " << got << "\n";
	if (got != 3) {
		std::cerr << "expected 3 but got " << got << "\n";
		return 1;
	}
	return 0;
}
