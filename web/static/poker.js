
const FACE = 'face';
const COVER = 'cover';

const SECOND = 1000;
const BUTTON_LEFT = 0;

const TAP_MAX_DURATION_MS = 300;
const MOVE_UPDATE_THROTTLE_MS = 30;

const DRAG_ZINDEX = 11000;

class ByValueIndex {
    constructor() {
        this.lookup = {};

        this.byId = {};
    }

    add(chip) {
        if (!(chip.info.val in this.lookup)) {
            this.lookup[chip.info.val] = new Array();
        }
        this.lookup[chip.info.val].push(chip);

        this.byId[chip.info.id] = chip;
    }

    get(val) {
        return this.lookup[val] || [];
    }
}

class TableItem {
    constructor(cls, info) {
        const elem = document.createElement('div');
        elem.id = `item-${info.id}`
        elem.classList.add(cls);

        this.info = info;
        this.elem = elem;

        this.setXY(info.x, info.y);

        // TODO: this should be removed
        elem.info = info;

        elem.ondragstart = () => false;
        // Make the item draggable
        elem.addEventListener('pointerdown', (e) => { onItemMouseDown(e, this); });
    }

    getElem() { return this.elem; }

    getRect() { return new Rect(this.elem); }

    id() { return this.elem.id; }

    isOwned() { return this.info.owner_id != ''; }

    isOwnedBy(user_id) { return this.info.owner_id == user_id; }

    handleDrop(slots) {}

    render() {}

    setXY(x, y) {
        this.info.x = x;
        this.info.y = y;
        this.elem.style.left = `${x}px`;
        this.elem.style.top = `${y}px`;
        return this;
    }


    update(src) {
        this.setXY(src.x, src.y);
        if (src.z_index && src.class != 'dealer') {
            this.elem.style.zIndex = src.z_index != 0 ? `${src.z_index}` : '';
        }
        this.info = src;
    }

    zIndex() { return parseInt(window.getComputedStyle(this.elem).zIndex); }

    setZIndex(zi) {
        this.info.z_index = zi;
        this.elem.style.zIndex = `${zi}`;
        return this;
    }
}

class Card extends TableItem {
    constructor(info) {
        super('card', info);

        this.elem.addEventListener('click', (e) => { onCardClick(e, this) });
        this.elem.addEventListener('dblclick', (e) => { onCardDblClick(e, this) });
        this.elem.addEventListener('touchend', (e) => {
            const currentTime = new Date().getTime();
            const tapInterval = currentTime - STATE.lastTapTime;
            if (tapInterval < TAP_MAX_DURATION_MS) {
                e.button = BUTTON_LEFT;
                onCardDblClick(e, this.elem);
            }
            STATE.lastTapTime = currentTime;
        });
    }

    handleDrop(slots) {
        const rect = this.getRect();
        for (let slot of slots) {
            if (!slot.playerElem) {
                continue;
            }
            const owner_id = slot.playerElem.info.owner_id;
            if (rect.centerWithin(slot.rect)) {
                if (STATE.current_uid == owner_id) {
                    takeCard(this);
                } else {
                    if (!this.isOwned()) {
                        ajax().success((resp) => { updateItem(resp.updated); }).
                            postJSON(`${window.location.pathname}/give_card?id=${this.info.id}&user_id=${owner_id}`);
                    }
                }
                return;
            }
        }
        const showSlot = document.getElementById('round-slot');
        if (rect.centerWithin(new Rect(showSlot))) {
            if (this.isOwned()) {
                showCard(this);
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

    render() {
        let text = '';
        let color = 'black';
        let side = this.info.side;
        let css = `card_${side}`;

        this.elem.style.borderColor = '';
        this.elem.classList.remove('card_cover', 'card_face', 'owned', 'was_owned');

        const owner_id = this.info.owner_id;
        if (this.isOwned()) {
            setCardBorder(this.elem, owner_id, 'owned');
        } else if (this.info.prev_owner_id != '') {
            setCardBorder(this.elem, this.info.prev_owner_id, 'was_owned');
        }
        if (side == FACE) {
            text = `${this.info.rank} ${this.info.suit}`;
            color = this.info.suit == '♥' || this.info.suit == '♦' ? 'red': 'black';
        }
        this.elem.innerText = text;
        this.elem.classList.add(css);
        this.elem.style.color = color;
    }
}

class Chip extends TableItem {
    constructor(info) {
        super('chip', info);
        this.elem.innerText = info.val;
        this.elem.classList.add(`chip-${info.color}`);
    }

    handleDrop(slots) {
        // XXX: accountChip has to be called in exactly in this handler.
        // Otherwise the following situation will not be handled correctly:
        // - when a chip that is being dragged stops under another chip.
        // In this case the event will be called with the top most item with
        // regard to z-index.
        accountChip(this, slots);
        slots.forEach(updateSlotsWithMoney);
    }
}

class Dealer extends TableItem {
    constructor(info) {
        super('dealer', info);
        this.elem.innerText = 'Dealer';
    }
}

class Player extends TableItem {
    constructor(info) {
        super('player', info);
        const player = STATE.players[info.owner_id];
        this.elem.classList.add(player.skin);
        this.elem.classList.add('fancy_text');
        this.elem.innerText = player.Name;

        const slot = document.getElementById(`slot-${player.index}`);
        slot.playerElem = this.elem;
    }

    render() {
        // HACK
        this.elem.style.zIndex = ''; // use from css
        this.elem.style.left = '';   // use from css
        this.elem.style.top = '';    // use from css
    };
}

const STATE = {
    current_uid: 0,

    players: {},
    theTable: null,

    chipIndex: new ByValueIndex(),

    items: {},

    socket: null,
    requestStats: new Stats(),

    lastTapTime: 0,

    tab_disconnected: false,
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

function ajax() { return new AJAX(STATE.requestStats); };

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
}

function stackChips(grabbedList, e) {
    // "stack" the chip to other chips under and nearby
    const grabbedIDs = new Set(grabbedList.map((it) => it.id()));
    for (let grabbed of grabbedList) {
        const nearBy = document.elementsFromPoint(e.clientX, e.clientY).filter((el) => {
            return el.info && el.info.class == 'chip' &&
                !grabbedIDs.has(el.id) &&
                grabbed.info.val == el.info.val;
        });
        for (let ch of nearBy) {
            const rect = new Rect(ch);
            if (grabbed.getRect().centerWithin(rect)) {
                const left = rect.left() + 3;
                const top = rect.top();
                grabbed.setXY(left, top);

                return;
            }
        }
    }
}

function handleCardDrop(card, slots) {
    const rect = card.getRect();
    for (let slot of slots) {
        if (!slot.playerElem) {
            continue;
        }
        const owner_id = slot.playerElem.info.owner_id;
        if (rect.centerWithin(slot.rect)) {
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

function isOnOtherPlayerSlot(item) {
    // XXX: document.elementsFromPoint does not return controls
    // if pointer-events: none, hence can't use it
    const itemRect = item.getRect();
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

function rearrangeZIndexOnDrop(grabbed) {
    if (grabbed.length === 0) {
        return;
    }
    if (grabbed[0].info.class == 'dealer') {
        return; // dealer is always on top
    }
    var itemRect = grabbed[0].getRect();
    // console.time('all_items');
    const grabbedIDs = new Set(grabbed.map((it) => it.id()));
    const elems = document.querySelectorAll('.chip, .card');
    const underList = [];
    // XXX: O(n) elements on the table - to optimize
    for (el of elems) {
         if (!grabbedIDs.has(el.id) &&
             itemRect.intersects(new Rect(el))
         ) {
             underList.push(STATE.items[el.id]);
         }
    }
    if (underList.length === 0) {
        return; // nothing is under
    }
    // sort elements by z-index descendig
    underList.sort((a, b) => b.zIndex() - a.zIndex());
    // underList should be sorted by z-index descendig
    let topmost = underList[0].info.z_index + 1;
    for (let it of grabbed.slice().reverse()) {
        it.setZIndex(topmost);
        topmost++;
    }
}

function isOffTheTable(item, x, y) {
    const itemRect = new Rect(item);
    const tableRect = STATE.theTable.getBoundingClientRect();

    const tableX = parseInt(x - tableRect.left);
    const tableY = parseInt(y - tableRect.top);
    // console.log('move coords', tableX, tableY, tableRect.left);
    return (tableX < 0 || tableX > tableRect.width - itemRect.width() / 2) ||
        (tableY < 0 || tableY > tableRect.height - itemRect.height() / 2)
}

// item state diagram:
// resting -> pick_up -> move -> ... -> click -> put_back
//            |---> click -> put_back
//            |---> click -> put_back -> pick_up -> dbl_click -> put_back
function onItemMouseDown(e, item) {
    if (e.button != BUTTON_LEFT) {
        return;
    }
    if (item.info.class == 'chip' && isOnOtherPlayerSlot(item)) {
        return;
    }

    let initialMouseX = e.clientX;
    let initialMouseY = e.clientY;

    let last_ms = new Date().getTime();

    const activePtrID = event.pointerId || 0;

    let grabbed = [item];
    if (e.shiftKey && item.info.class == 'chip') {
        const elems = document.elementsFromPoint(e.clientX, e.clientY).filter(
            (el) => el.id != item.id() && el.matches('.chip') && el.info.val == item.info.val
        );
        grabbed = grabbed.concat(elems.map((el) => STATE.items[el.id]));
    }
    for (it of grabbed) {
        it.initialX = parseInt(it.getElem().style.left);
        it.initialY = parseInt(it.getElem().style.top);
        it.initialZIndex = it.zIndex();
    }
    // push items to top when they are being dragged
    grabbed.forEach((it) => { it.setZIndex(DRAG_ZINDEX + it.initialZIndex); });

    function onMouseMove(event) {
        if (activePtrID != event.pointerId) {
            return;
        }
        if (grabbed.length < 1) {
            return;
        }
        if (isOffTheTable(item.getElem(), event.clientX, event.clientY)) {
            return; // disallow to move items outside the table
        }

        const deltaX = event.clientX - initialMouseX;
        const deltaY = event.clientY - initialMouseY;
        for (it of grabbed) {
            const left = parseInt(it.initialX + deltaX);
            const top = parseInt(it.initialY + deltaY);

            it.setXY(left, top);
        }

        const now_ms = new Date().getTime();
        if (now_ms - last_ms < MOVE_UPDATE_THROTTLE_MS) {
            return; // throttle down updates to handle slower connections
        }
        last_ms = now_ms;

        ajax().postJSON(`${window.location.pathname}/update_many`,
            { items: grabbed.map((it) => it.info) });
    }
    document.addEventListener('pointermove', onMouseMove);
    document.addEventListener('pointerup', (e) => {
        if (activePtrID != e.pointerId) {
            return;
        }
        // cleanup for this drag-n-drop
        document.removeEventListener('pointermove', onMouseMove);
        // restore z-index
        grabbed.forEach((it) => { it.setZIndex(it.initialZIndex); });

        const deltaX = e.clientX - initialMouseX;
        const deltaY = e.clientY - initialMouseY;
        if (deltaX == 0 && deltaY == 0) {
            // all chips are put on the table
            grabbed = [];
            return; // no real move happened, no need to post updates
        }
        rearrangeZIndexOnDrop(grabbed);
        if (item.info.class == 'chip') {
            stackChips(grabbed, e);
        }

        ajax().success((resp) => {
            grabbed.forEach((it) => {
                const slots = document.querySelectorAll('.slot');
                it.handleDrop(slots);
            });
            // all chips are put on the table
            grabbed = [];
        }).postJSON(`${window.location.pathname}/update_many`,
            { 'items': grabbed.map((it) => it.info) });
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
    if (e.button != BUTTON_LEFT) {
        return;
    }
    if (card.isOwned() && !card.isOwnedBy(STATE.current_uid)) {
        return; // can't turn other player cards cards
    }
    console.log('mouse double click', card.info.id);
    card.info.side = card.info.side == COVER ? FACE: COVER;
    ajax().success((resp) => {
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/update`, card.info)
}

function onCardClick(e, card) {
    if (e.button != BUTTON_LEFT) {
        return;
    }
    if (e.ctrlKey || e.metaKey) {
        takeCard(card);
    }
    if (e.shiftKey) {
        showCard(card);
    }
}

function accountChip(chip, slots) {
    if (!chip) {
        return;
    }
    chip.getElem().classList.remove('forbidden');
    const rect = chip.getRect();
    for (let slot of slots) {
        if (!slot.chips) {
            continue;
        }
        if (chip.id() in slot.chips) {
            delete slot.chips[chip.id()];
        }
        if (rect.centerWithin(slot.rect)) {
            slot.chips[chip.id()] = chip.getElem();
            if (slot.playerElem && !isOwnedBy(slot.playerElem.info, STATE.current_uid)) {
                chip.getElem().classList.add('forbidden');
            }
            return; // slots do not intersect
        }
    }
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
    if (!src) {
        console.log('WARN updateItem:', src);
        return null;
    }
    if (!src.id) {
        console.log('WARN bad id', src);
        return null;
    }
    let item = STATE.items[`item-${src.id}`];
    if (!item) {
        item = createItem(src);
    }
    item.update(src);
    item.render();
    return item;
}

function updateItems(items) {
    const slots = document.querySelectorAll('.slot');
    for (let it of items) {
        const item = updateItem(it);
        // XXX: optimization - do not account while chip is moving
        // to reduce the number of useless calls to accountChip
        // accountChip takes noticable time if there are many chips moving at once.
        // It is enough to call it when the stack has been put on the table
        const isMoving = it.z_index >= DRAG_ZINDEX;
        if (it.class == 'chip' && !isMoving) {
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
            return new Card(info);
        case 'chip':
            it = new Chip(info);
            STATE.chipIndex.add(it.getElem());
            return it;
        case 'dealer':
            return new Dealer(info);
        case 'player':
            return new Player(info);
        default:
            throw new Exception(`unknown item class: ${info.class}`)
        }
    }();
    STATE.items[item.id()] = item;
    STATE.theTable.appendChild(item.getElem());
    return item;
}

function takeCard(card) {
    if (card.isOwned()) {
        return; // already owned
    }
    ajax().success((resp) => {
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/take_card`, {id: card.info.id});
}

function showCard(card) {
    if (!card.isOwnedBy(STATE.current_uid)) {
        return; // can't show not owned cards
    }
    ajax().success((resp) => {
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/show_card`, {id: card.info.id});
}

function handlePush(resp) {
    switch (resp.type) {
    case 'disconnected':
        STATE.tab_disconnected = true;
        break;
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
    const proto = window.isSecureContext ? 'wss': 'ws';
    const sock = new WebSocket(`${proto}://${window.location.host}${window.location.pathname}/listen`);

    sock.onopen = () => {
        console.log('websocket connected');
        hideElem(document.getElementById('error-banner'));
        STATE.tab_disconnected = false;
    };
    sock.onclose = () => {
        console.log('websocket disconnected');
        if (STATE.tab_disconnected) {
            showError('OFFLINE. You connected from another browser or browser tab');
            return;
        }
        showError('OFFLINE. Connection dropped. Try to refresh');
        setTimeout(() => { socket = listenPushes(); }, 10 * SECOND);
    };
    sock.onerror = (err) => {
        console.error('websocket error:', err);
    };
    sock.onmessage = (e) => {
        let resp = null;
        try {
            resp = JSON.parse(e.data)
        } catch (ex) {
            // non-JSON payload?
            console.log("error: unknown payload", ex, e.data);
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
    slots.forEach((slot) => {
        slot.chips = {};
        slot.rect = new Rect(slot);
    });

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
