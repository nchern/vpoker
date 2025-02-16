// Rect represents a rect over an HTML element
class Rect {
    constructor(item) {
        this.item = item;
    }

    intersects(other) {
        return !(
            this.left + this.width <= other.left ||     // this is left of other
            other.left + other.width <= this.left ||    // other is left of this
            this.top + this.height <= other.top ||      // this is above other
            other.top + other.height <= this.top        // other is above this
        );
    }

    left() { return parseInt(window.getComputedStyle(this.item).left); }

    top() { return parseInt(window.getComputedStyle(this.item).top); }

    width() { return parseInt(window.getComputedStyle(this.item).width); }

    height() {return parseInt(window.getComputedStyle(this.item).height); }

    centerX() {
        return this.left() + this.width()/2;
    }
    centerY() {
        return this.top() + this.height()/2;
    }

    contains(x, y) {
        return x >= this.left() && x <= this.left() + this.width() &&
                y >= this.top() && y <= this.top() + this.height();
    }

    centerWithin(rect) {
        return this.centerX() >= rect.left() && this.centerX() <= rect.left() + rect.width() &&
                this.centerY() >= rect.top() && this.centerY() <= rect.top() + rect.height();
    }

    distance(rect) {
        const d = Math.pow(this.centerX() - rect.centerX(), 2) + Math.pow(this.centerY() - rect.centerY(), 2);
        return Math.sqrt(d);
    }
}

class AJAX {
    constructor(stats) {
        this.stats = stats;
        this.successHandler = null;
        this.errorHandler = null;
    }

    success(callback) {
        this.successHandler = callback;
        return this;
    }

    error(callback) {
        this.errorHandler = callback;
        return this;
    }

    async request(method, url, body = null) {
        try {
            const startedAt = new Date().getTime();
            const response = await fetch(url, {
                method: method,
                headers: { 'Content-Type': 'application/json' },
                body: body ? JSON.stringify(body) : null,
            });
            const duration = new Date().getTime() - startedAt;
            this.stats.add(duration);
            if (!response.ok) {
                const err = new Error('HTTP error');
                err.status = response.status;
                err.body = response.text();
                throw err;
            }
            const data = await response.json();
            if (this.successHandler) {
                this.successHandler(data);
            }
        } catch (error) {
            if (this.errorHandler) {
                this.errorHandler(error);
                return;
            }
            console.error('AJAX.fetch error: ', error);
        }
    }

    get(url) { this.request('GET', url); }
    postJSON(url, obj) { this.request('POST', url, obj); }
}

// Stats represent a stats collector
class Stats {
  constructor() {
    this.buffer = [];
    this.capacity = 10;
  }

  add(integer) {
    if (this.buffer.length >= this.capacity) {
      this.buffer.shift();
    }
    this.buffer.push(integer);
  }

  mean() {
    if (this.buffer.length === 0) return null;
    return this.buffer.reduce((sum, num) => sum + num, 0) / this.buffer.length;
  }

  min() {
    return this.buffer.length === 0 ? null : Math.min(...this.buffer);
  }

  max() {
    return this.buffer.length === 0 ? null : Math.max(...this.buffer);
  }

  median() {
    if (this.buffer.length === 0) return null;

    const sorted = [...this.buffer].sort((a, b) => a - b);
    const mid = Math.floor(sorted.length / 2);

    return sorted.length % 2 === 0
      ? (sorted[mid - 1] + sorted[mid]) / 2
      : sorted[mid];
  }
}

function isKeyPressed(e, key) {
    try {
        return e.key.toLowerCase() === key;
    } catch {
        return false;
    }
}

function showElem(elem) {
    elem.style.display = 'block';
}

function hideElem(elem) {
    elem.style.display = 'none';
}

function isPortraitMode() { return window.innerWidth < window.innerHeight; }
