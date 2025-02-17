
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

const STATE = {
    'current_uid': 0,

    'players': {},
    'theTable': null,

    'chipIndex': new ByValueIndex(),

    'socket': null,
    'requestStats': new Stats(),

    'lastTapTime': 0,

    'tab_disconnected': false,
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
    const grabbedIDs = new Set(grabbedList.map((it) => it.id));
    for (let grabbed of grabbedList) {
        const nearBy = document.elementsFromPoint(e.clientX, e.clientY).filter((it) => {
            return it.info && it.info.class == 'chip' &&
                !grabbedIDs.has(it.id) &&
                grabbed.info.val == it.info.val;
        });
        // for (let ch of STATE.chipIndex.get(it.info.val)) {
        for (let ch of nearBy) {
            const rect = new Rect(ch);
            if ((new Rect(grabbed)).centerWithin(rect)) {
                const left = rect.left() + 3;
                const top = rect.top();

                grabbed.style.left = `${left}px`;
                grabbed.style.top = `${top}px`;

                grabbed.info.x = left;
                grabbed.info.y = top;
                return;
            }
        }
    }
}

function handleCardDrop(card, slots) {
    const rect = new Rect(card);
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

function rearrangeZIndexOnDrop(grabbed) {
    if (grabbed.length === 0) {
        return;
    }
    if (grabbed[0].info.class == 'dealer') {
        return; // dealer is always on top
    }
    var itemRect = new Rect(grabbed[0]);
    // console.time('all_items');
    const grabbedIDs = new Set(grabbed.map((it) => it.id));
    const items = document.querySelectorAll('.chip, .card');
    const underList = [];
    // XXX: O(n) elements on the table - to optimize
    for (it of items) {
         if (!grabbedIDs.has(it.id) &&
             itemRect.intersects(new Rect(it))
         ) {
             underList.push(it);
         }
    }
    if (underList.length === 0) {
        return; // nothing is under
    }
    // sort elements by z-index descendig
    underList.sort((a, b) => parseInt(b.style.zIndex) - parseInt(a.style.zIndex));
    // underList should be sorted by z-index descendig
    let topmost = underList[0].info.z_index + 1;
    for (let it of grabbed.slice().reverse()) {
        it.info.z_index = topmost;
        it.style.zIndex = `${it.info.z_index}`;
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
    console.log('mouse down', item.info.id);

    let initialMouseX = e.clientX;
    let initialMouseY = e.clientY;

    let last_ms = new Date().getTime();

    const activePtrID = event.pointerId || 0;

    let grabbed = [item];
    if (e.shiftKey && item.info.class == 'chip') {
        grabbed = grabbed.concat(document.elementsFromPoint(e.clientX, e.clientY).filter(
            (it) => it.id != item.id && it.matches('.chip') && it.info.val == item.info.val
        ));
    }
    for (it of grabbed) {
        it.initialX = parseInt(it.style.left);
        it.initialY = parseInt(it.style.top);
        it.initialZIndex = parseInt(window.getComputedStyle(it).zIndex);
    }
    // push items to top when they are being dragged
    grabbed.forEach((it) => { setItemZIndex(it, DRAG_ZINDEX + it.initialZIndex); });

    function onMouseMove(event) {
        if (activePtrID != event.pointerId) {
            return;
        }
        if (grabbed.length < 1) {
            return;
        }

        // console.log('move coords', tableX, tableY, tableRect.left);
        if (isOffTheTable(item, event.clientX, event.clientY)) {
            return; // disallow to move items outside the table
        }

        const deltaX = event.clientX - initialMouseX;
        const deltaY = event.clientY - initialMouseY;
        for (it of grabbed) {
            const left = parseInt(it.initialX + deltaX);
            const top = parseInt(it.initialY + deltaY);

            it.info.x = left;
            it.info.y = top;
            it.style.left = `${left}px`;
            it.style.top = `${top}px`;
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
        grabbed.forEach((it) => { setItemZIndex(it, it.initialZIndex); });

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
            grabbed.forEach((it) => { handleItemDrop(it); });
            // all chips are put on the table
            grabbed = [];
        }).postJSON(`${window.location.pathname}/update_many`,
            { 'items': grabbed.map((it) => it.info) });
    }, { once: true });
}

function setItemZIndex(item, zi) {
    item.info.z_index = zi;
    item.style.zIndex = `${zi}`;
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
    if (isOwned(card.info) && !isOwnedBy(card.info, STATE.current_uid)) {
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
    console.log('mouse click', card.info.id);
    if (e.ctrlKey || e.metaKey) {
        takeCard(card);
    }
    if (e.shiftKey) {
        showCard(card);
    }
}

function newCard(info, x, y) {
    const card = newItem('card', info, x, y);
    card.addEventListener('click', (e) => { onCardClick(e, card) });
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
        if (chip.id in slot.chips) {
            delete slot.chips[chip.id];
        }
        if (rect.centerWithin(slot.rect)) {
            slot.chips[chip.id] = chip;
            if (slot.playerElem && !isOwnedBy(slot.playerElem.info, STATE.current_uid)) {
                chip.classList.add('forbidden');
            }
            return; // slots do not intersect
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
        return null;
    }
    if (src.id === null || src.id === undefined) {
        console.log('warn bad id', src);
        return null;
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

function getChipsFromPoint(x, y) {
    res = [];
    for (c of Object.values(STATE.chipIndex.byId)) {
        const rect = new Rect(c);
        if (rect.contains(x, y)) {
            res.push(c);
        }
    }
    res.sort((a, b) => b.info.zIndex - a.info.zIndex );
    return res;
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
