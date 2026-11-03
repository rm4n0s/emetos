#include <stdio.h>

static int add(int a, int b) { return a + b; }

int main(void) {
	printf("adding 40 and 2\n");
	return add(40, 2) == 42 ? 0 : 1;
}
