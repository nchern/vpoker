let wakeLock;

async function requestWakeLock() {
    try {
        if (!navigator.wakeLock) {
            console.info('wakeLock API is not supported')
            return;
        }
        wakeLock = await navigator.wakeLock.request('screen');
        wakeLock.addEventListener('release', () => {
            console.log('Wake lock released');
        });
        console.log('Wake lock is active');
    } catch (err) {
        console.error('Wake lock request failed:', err);
    }
}

function initWakeLock() {
    if (!window.isSecureContext) {
        return; // wake lock works only on https
    }
    // Request wake lock when the page loads
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') {
            requestWakeLock();
        } else if (wakeLock) {
            wakeLock.release();
            wakeLock = null;
        }
    });
}
