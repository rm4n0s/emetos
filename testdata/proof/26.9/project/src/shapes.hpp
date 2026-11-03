#pragma once
#include <string>

struct Rect {
	int w, h;
};

int area(const Rect &r);
int perimeter(const Rect &r);
Rect scaled(const Rect &r, int factor);
std::string describe(const Rect &r);
