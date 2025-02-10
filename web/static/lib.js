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
