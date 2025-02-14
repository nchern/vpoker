
const FACE = 'face';
const COVER = 'cover';

const SECOND = 1000;
const BUTTON_LEFT = 0;

const TAP_MAX_DURATION_MS = 300;
const MOVE_UPDATE_THROTTLE_MS = 30;

class ByValueIndex {
    constructor() {
        this.lookup = {};
    }

    add(chip) {
        if (!(chip.info.val in this.lookup)) {
            this.lookup[chip.info.val] = new Array();
        }
        this.lookup[chip.info.val].push(chip);
    }

    get(val) {
        return this.lookup[val] || [];
    }
}

const STATE = {
    'current_uid': 0,

    'players': {},
    'theTable': null,

    'chipIndex': new ByValueIndex(),

    'socket': null,
    'requestStats': new Stats(),

    'lastTapTime': 0,
}

function getSession() {
    const cookies = document.cookie.split('; ');
    const sessionCookie = cookies.find(cookie => cookie.startsWith('session='));
    if (sessionCookie) {
        const encodedValue = sessionCookie.split('=')[1];
        return JSON.parse(atob(encodedValue));
    }
    return {'user_id': '', 'name': ''};
}

class AJAX {
    constructor() {
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
            STATE.requestStats.add(duration);
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

function ajax() { return new AJAX() };

// Rect represents a rect over an HTML element
class Rect {
    constructor(item) {
        this.item = item;
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

function isOwned(info) { return info.owner_id != ''; }

function isOwnedBy(info, user_id) { return info.owner_id == user_id; }

function handleChipDrop(chip, slots) {
    // XXX: accountChip has to be called in exactly in this handler.
    // Otherwise the following situation will not be handled correctly:
    // - when a chip that is being dragged stops under another chip.
    // In this case the event will be called with the top most item with
    // regard to z-index.
    accountChip(chip, slots);
    slots.forEach(updateSlotsWithMoney);

    // "stack" the chip to other chips under and nearby
    const thisRect = new Rect(chip);
    for (let ch of STATE.chipIndex.get(chip.info.val)) {
        const rect = new Rect(ch);
        if (ch.id != chip.id && thisRect.centerWithin(rect)) {
            const left = rect.left() + 2;
            const top = rect.top();

            chip.style.left = `${left}px`;
            chip.style.top = `${top}px`;

            chip.info.x = left;
            chip.info.y = top;
            return;
        }
    }
}

function handleCardDrop(card, slots) {
    const rect = new Rect(card);
    for (let slot of slots) {
        if (!slot.playerElem) {
            continue;
        }
        const slotRect = new Rect(slot);
        const owner_id = slot.playerElem.info.owner_id;
        if (rect.centerWithin(slotRect)) {
            if (STATE.current_uid == owner_id) {
                takeCard(card);
            } else {
                if (!isOwned(card.info)) {
                    ajax().success((resp) => { updateItem(resp.updated); }).
                        postJSON(`${window.location.pathname}/give_card?id=${card.info.id}&user_id=${owner_id}`);
                }
            }
            return;
        }
    }
    const showSlot = document.getElementById('round-slot');
    if (rect.centerWithin(new Rect(showSlot))) {
        if (isOwned(card.info)) {
            showCard(card);
        }
        // TODO: disable auto open in case of non-owned cards
        // currently this conflicts with turning a card by a double-click
        // else {
        //     card.info.side = FACE;
        //     ajax().success((resp) => { updateItem(resp.updated); }).
        //         postJSON(`${window.location.pathname}/update`, card.info);
        // }
    }
}

function handleItemDrop(item) {
    const slots = document.querySelectorAll('.slot');
    switch (item.info.class) {
    case 'chip':
        handleChipDrop(item, slots);
        break;
    case 'card':
        handleCardDrop(item, slots);
        break;
    }
}

function isOnOtherPlayerSlot(item) {
    // XXX: document.elementsFromPoint does not return controls
    // if pointer-events: none, hence can't use it
    const itemRect = new Rect(item);
    const current_uid = STATE.current_uid;
    const slots = document.querySelectorAll('.slot');
    for (let slot of slots) {
        if (!slot.playerElem) {
            continue;
        }
        const rect = new Rect(slot);
        if (itemRect.centerWithin(rect)) {
            if (!isOwnedBy(slot.playerElem.info, current_uid)) {
                return true;
            }
        }
    }
    return false;
}

function rearrangeZIndexOnDrop(event, item) {
    if (item.info.class == 'dealer') {
        return; // dealer is always on top
    }
    // elementsFromPoint returns elements sorted by z-index descendig
    const underList = document.elementsFromPoint(event.clientX, event.clientY).filter((el) => {
        return el.id != item.id && (el.classList.contains('chip') || el.classList.contains('card'));
    });
    let prev = item;
    // underList should be sorted by z-index descendig
    for (let c of underList) {
        if (c.info.z_index > prev.info.z_index) {
            [prev.info.z_index, c.info.z_index] = [c.info.z_index, prev.info.z_index];
            c.style.zIndex = `${c.info.z_index}`;
            prev.style.zIndex = `${prev.info.z_index}`;
        }
        prev = c;
    }
}

function onItemMouseDown(e, item) {
    if (e.button != BUTTON_LEFT) {
        return;
    }
    if (item.info.class == 'chip' && isOnOtherPlayerSlot(item)) {
        return;
    }

    let initialMouseX = e.clientX;
    let initialMouseY = e.clientY;

    let initialItemX = parseInt(item.style.left);
    let initialItemY = parseInt(item.style.top);

    let initialZIndex = parseInt(window.getComputedStyle(item).zIndex);

    item.style.zIndex = '1000'; // push this item to top when being dragged
    item.info.z_index = 1000;

    const activePtrID = event.pointerId || 0;

    let last_ms = new Date().getTime();

    function onMouseMove(event) {
        if (activePtrID != event.pointerId) {
            return;
        }

        const deltaX = event.clientX - initialMouseX;
        const deltaY = event.clientY - initialMouseY;

        const left = parseInt(initialItemX + deltaX);
        const top = parseInt(initialItemY + deltaY);

        const itemRect = new Rect(item);
        const tableRect = new Rect(STATE.theTable);

        if ((left < 0 || left > tableRect.width() - itemRect.width()) ||
            (top < 0 || top > tableRect.height() - itemRect.height())
        ) {
            return; // disallow to move items outside the table
        }

        item.style.left = `${left}px`;
        item.style.top = `${top}px`;

        item.info.x = left;
        item.info.y = top;

        const now_ms = new Date().getTime();
        if (now_ms - last_ms < MOVE_UPDATE_THROTTLE_MS) {
            return; // throttle down updates to handle slower connections
        }
        last_ms = now_ms;

        ajax().postJSON(`${window.location.pathname}/update`, item.info);
    }
    document.addEventListener('pointermove', onMouseMove);

    document.addEventListener('pointerup', (event) => {
        if (activePtrID != event.pointerId) {
            return;
        }
        item.info.z_index = initialZIndex;
        item.style.zIndex = `${initialZIndex}`;

        rearrangeZIndexOnDrop(event, item);

        ajax().success((resp) => {
            handleItemDrop(item);
        }).postJSON(`${window.location.pathname}/update`, item.info);

        // cleanup for this drag-n-drop
        document.removeEventListener('pointermove', onMouseMove);
    }, { once: true });
}

function newItem(cls, info, x, y) {
    const item = document.createElement('div');
    item.classList.add(cls);

    item.id = `item-${info.id}`
    item.info = info;
    item.style.top = `${y}px`;
    item.style.left = `${x}px`;

    item.ondragstart = () => false;
    // Make the item draggable
    item.addEventListener('pointerdown', (e) => { onItemMouseDown(e, item); });

    item.render = () => {};

    return item;
}

function setCardBorder(card, user_id, cls) {
    card.classList.add(cls);
    card.style.borderColor = STATE.players[user_id].color || 'black';
}

function renderCard(card) {
    let text = '';
    let color = 'black';
    let side = card.info.side;
    let css = `card_${side}`;

    card.style.borderColor = '';
    card.classList.remove('card_cover', 'card_face', 'owned', 'was_owned');

    const owner_id = card.info.owner_id;
    if (isOwned(card.info)) {
        setCardBorder(card, owner_id, 'owned');
    } else if (card.info.prev_owner_id != '') {
        setCardBorder(card, card.info.prev_owner_id, 'was_owned');
    }
    if (side == FACE) {
        text = `${card.info.rank} ${card.info.suit}`;
        color = card.info.suit == '♥' || card.info.suit == '♦' ? 'red': 'black';
    }
    card.innerText = text;
    card.classList.add(css);
    card.style.color = color;
}

function onCardDblClick(e, card) {
    // console.log('DEBUG onCardDblClick', e.button);
    if (e.button != BUTTON_LEFT) {
        return;
    }
    if (isOwned(card.info) && !isOwnedBy(card.info, STATE.current_uid)) {
        return; // can't turn other player cards cards
    }
    card.info.side = card.info.side == COVER ? FACE: COVER;
    ajax().success((resp) => {
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/update`, card.info)
}

function newCard(info, x, y) {
    const card = newItem('card', info, x, y);
    card.addEventListener('click', (e) => {
        if (e.button != BUTTON_LEFT) {
            return;
        }
        if (e.ctrlKey || e.metaKey) {
            takeCard(card);
        }
        if (e.shiftKey) {
            showCard(card);
        }
    });
    card.addEventListener('dblclick', (e) => { onCardDblClick(e, card) });

    card.addEventListener('touchend', (e) => {
        const currentTime = new Date().getTime();
        const tapInterval = currentTime - STATE.lastTapTime;
        if (tapInterval < TAP_MAX_DURATION_MS) {
            e.button = BUTTON_LEFT;
            onCardDblClick(e, card);
        }
        STATE.lastTapTime = currentTime;
    });

    card.render = () => { renderCard(card); };
    card.render();
    return card;
}

function accountChip(chip, slots) {
    if (!chip) {
        return;
    }
    chip.classList.remove('forbidden');
    const rect = new Rect(chip);
    for (let slot of slots) {
        if (!slot.chips) {
            continue;
        }
        slotRect = new Rect(slot);
        if (chip.id in slot.chips) {
            delete slot.chips[chip.id];
        }
        if (rect.centerWithin(slotRect)) {
            slot.chips[chip.id] = chip;
            if (slot.playerElem && !isOwnedBy(slot.playerElem.info, STATE.current_uid)) {
                chip.classList.add('forbidden');
            }
            // console.log(`${chip.info.class} id=${chip.id} within player ${slot.dataset.index} slot`);
        } else {
            // console.log(`${chip.info.class} id=${chip.id} outside of any player slot`);
        }
    }
}

function newChip(info, x, y) {
    const chip = newItem('chip', info, x, y);
    chip.classList.add(`chip-${info.color}`);
    chip.innerText = info.val;

    return chip;
}

function newDealer(info, x, y) {
    const item = newItem('dealer', info, x, y);
    item.innerText = 'Dealer';
    return item;
}

function newPlayer(info, x, y) {
    const item = newItem('player', info, x, y);
    const player = STATE.players[info.owner_id];
    item.classList.add(player.skin);
    item.classList.add('fancy_text');
    item.innerText = player.Name;

    const slot = document.getElementById(`slot-${player.index}`);
    slot.playerElem = item;

    item.render = () => {
        // HACK
        item.style.zIndex = ''; // use from css
        item.style.left = '';   // use from css
        item.style.top = '';    // use from css
    };
    item.render();
    return item;
}

function updateSlotsWithMoney(slot) {
    if (!slot.chips) {
        return;
    }
    const vals = Object.values(slot.chips).map(chip => chip.info.val);
    const total = vals.reduce((s, x) => s + x, 0);;
    if (slot.playerElem) {
        const player = STATE.players[slot.playerElem.info.owner_id];
        slot.playerElem.innerText = `${player.Name}: ${total}$`;
        slot.innerText = '';
    } else {
        slot.innerText = `${total}$`;
    }
}

function updateItem(src) {
    if (src === null || src === undefined) {
        return;
    }
    if (src.id === null || src.id === undefined) {
        console.log('warn bad id', src);
        return;
    }
    let item = document.getElementById(`item-${src.id}`);
    if (item == null) {
        item = createItem(src);
    }
    item.info = src;
    item.style.top = `${src.y}px`;
    item.style.left = `${src.x}px`;
    if (src.z_index != null && src.z_index != undefined &&
        src.class != 'dealer') {
        item.style.zIndex = src.z_index != 0 ? `${src.z_index}` : '';
    }
    item.render();
}

function updateItems(items) {
    const slots = document.querySelectorAll('.slot');
    for (let it of items) {
        updateItem(it);
        if (it.class == 'chip') {
            const item = document.getElementById(`item-${it.id}`);
            accountChip(item, slots);
        }
    }
    slots.forEach(updateSlotsWithMoney);
}

function updateTable(resp) {
    STATE.players = resp.players;
    updateItems(resp.items);
}

function createItem(info) {
    let item = function() {
        switch (info.class) {
        case 'card':
            return newCard(info, info.x, info.y);
        case 'chip':
            it = newChip(info, info.x, info.y);
            STATE.chipIndex.add(it);
            return it;
        case 'dealer':
            return newDealer(info, info.x, info.y);
        case 'player':
            return newPlayer(info, info.x, info.y);
        default:
            throw new Exception(`unknown item class: ${info.class}`)
        }
    }();
    STATE.theTable.appendChild(item);
    return item;
}

function takeCard(card) {
    if (isOwned(card.info)) {
        return; // already owned
    }
    ajax().success((resp) => {
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/take_card`, {id: card.info.id});
}

function showCard(card) {
    if (!isOwnedBy(card.info, STATE.current_uid)) {
        return; // can't show not owned cards
    }
    ajax().success((resp) => {
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/show_card`, {id: card.info.id});
}

function handlePush(resp) {
    switch (resp.type) {
    case 'player_kicked':
        for (p of Object.values(resp.players)) {
            if (p.user_id == STATE.current_uid) {
                showError('You have been kicked!')
                continue;
            }
            const slot = document.getElementById(`slot-${p.index}`);
            slot.playerElem.remove();
            slot.playerElem = null;
            delete STATE.players[p.user_id]
        }
        break;
    case 'player_joined':
        updateTable(resp);
        break;
    case 'update_items':
        updateItems(resp.items);
        break;
    case 'refresh':
        location.reload();
        break;
    default:
        console.log("push unknown:", resp);
    }
}

function listenPushes() {
    const sock = new WebSocket(`ws://${window.location.host}${window.location.pathname}/listen`);
    sock.onopen = () => {
        console.log('websocket connected');
        hideElem(document.getElementById('error-banner'));
    };
    sock.onclose = () => {
        console.log('websocket disconnected');
        showError('OFFLINE. Try to refresh');
        setTimeout(() => { socket = listenPushes(); }, 10 * SECOND);
    };
    sock.onerror = (err) => {
        console.error('websocket error:', err);
    };
    sock.onmessage = (event) => {
        let resp = null;
        try {
            resp = JSON.parse(event.data)
        } catch (e) {
            // non-JSON payload?
            console.log("error: unknown payload", e, event.data);
            return;
        }
        handlePush(resp);
    };
    return sock;
}

function showError(text) {
    const banner = document.getElementById('error-banner');
    banner.innerHTML = `<p>${text}</p>`;
    showElem(banner);
    return banner;
}

function blockTable(table) {
    showError('Portrait mode is not supported. Switch to landscape!');
    for (let elem of table.children) {
        hideElem(elem);
    }
}

function logStats() {
    const stats = `min_ms=${STATE.requestStats.min()}` +
        `&max_ms=${STATE.requestStats.max()}` +
        `&median_ms=${STATE.requestStats.median()}`;
    ajax().get(`/log?type=client_stats&${stats}`);
}

function start() {
    const slots = document.querySelectorAll('.slot');
    slots.forEach((slot) => { slot.chips = {}; });

    STATE.current_uid = getSession().user_id;
    STATE.theTable = document.getElementById('card-table');
    window.addEventListener("resize", function() {
        if (isPortraitMode()) {
            blockTable(STATE.theTable);
        } else {
            location.reload();
        }
    });
    if (isPortraitMode()) {
        blockTable(STATE.theTable);
        return;
    } else {
        hideElem(document.getElementById('error-banner'));
    }
    setInterval(logStats, 15 * SECOND);

    ajax().success((resp) => {
        console.info('initial table fetch:', resp);
        updateTable(resp);
        STATE.socket = listenPushes();
    }).get(`${window.location.pathname}/state?cw=${window.screen.availWidth}&ch=${window.screen.availHeight}&iw=${window.innerWidth}&ih=${window.innerHeight}`);
}
