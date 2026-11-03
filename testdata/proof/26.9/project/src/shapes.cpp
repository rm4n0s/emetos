#include "shapes.hpp"

int area(const Rect &r) { return r.w * r.h; }

int perimeter(const Rect &r) { return 2 * (r.w + r.h); }

Rect scaled(const Rect &r, int factor) {
	// bug: only the width is scaled
	return Rect{r.w * factor, r.h};
}

std::string describe(const Rect &r) {
	return std::to_string(r.w) + "x" + std::to_string(r.h);
}
