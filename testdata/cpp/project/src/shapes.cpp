#include "shapes.h"

Rectangle makeRectangle(int width, int height) {
    return Rectangle{width, height};
}

int area(const Rectangle &r) {
    return r.width * r.height;
}
