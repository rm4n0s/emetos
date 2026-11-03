#include <cstdio>
#include <cstring>
#include <string>

#include "shapes.hpp"

static int failures = 0;

#define CHECK_EQ(want, got)                                                              \
	do {                                                                                 \
		if ((want) != (got)) {                                                           \
			std::printf("%s:%d: Failure\n  expected %d, got %d\n", __FILE__, __LINE__, (int)(want), (int)(got)); \
			failures++;                                                                  \
		}                                                                                \
	} while (0)

static void square() { CHECK_EQ(36, area(Rect{6, 6})); }

static void perimeter_of_a_rectangle() { CHECK_EQ(14, perimeter(Rect{3, 4})); }

static void scaled_rectangle() {
	Rect r = scaled(Rect{3, 4}, 3);
	CHECK_EQ(108, area(r));
}

static void describe_a_rectangle() {
	std::string d = describe(Rect{3, 4});
	std::printf("described as %s\n", d.c_str());
	CHECK_EQ(1, d == "3x4");
}

int main(int argc, char **argv) {
	std::string which = argc > 1 ? argv[1] : "";
	if (which == "square") {
		square();
	} else if (which == "perimeter") {
		perimeter_of_a_rectangle();
	} else if (which == "scaled") {
		scaled_rectangle();
	} else if (which == "describe") {
		describe_a_rectangle();
	} else if (which == "window") {
		std::printf("no display available\n");
		return 77;
	}
	return failures == 0 ? 0 : 1;
}
