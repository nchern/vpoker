
const FACE = 'face';
const COVER = 'cover';

const BUTTON_LEFT = 0;

let players = {};

let isKeyTPressed = false;
let isKeyOPressed = false;

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
        this.onSuccess = function(){};
        this.onError = function(){};
    }

    success(onSuccess) {
        this.onSuccess = onSuccess;
        return this;
    }

    error(onError) {
        this.onError = onError;
        return this;
    }

    get(url) {
        const xhr = new XMLHttpRequest();
        const onSuccess = this.onSuccess;
        const onError = this.onError;
        xhr.open('GET', url, true);
        // Handle network-level errors
        xhr.onerror = function () {
            console.error('XHR network error: unable to complete the request');
            onError(0, 'network error');
        };
        xhr.onreadystatechange = function () {
            if (xhr.readyState != 4) {
                return;
            }
            if (xhr.status === 0) {
                return;
            }
            if (xhr.status === 200) {
                const contentType = xhr.getResponseHeader('Content-Type');
                if (contentType && contentType.includes('application/json')) {
                    let resp = null;
                    try {
                        resp = JSON.parse(xhr.responseText)
                    } catch (e) {
                        console.error(`XHR JSON.parse error: unable to parse JSON: ${e.message}`);
                        onError(-1, e.message);
                        return;
                    }
                    onSuccess(resp);
                    return;
                }
                onSuccess(xhr.responseText);
                return;
            }
            // handle HTTP error
            console.error(`XHR HTTP error: ${xhr.status}: ${xhr.statusText} ${url}`);
            onError(xhr.status, xhr.responseText)
        };
        xhr.send();
    }

    postJSON(url, obj) {
        const xhr = new XMLHttpRequest();
        const onSuccess = this.onSuccess;
        const onError = this.onError;
        xhr.open('POST', url, true);
        xhr.setRequestHeader('Content-Type', 'application/json');
        // Handle network-level errors
        xhr.onerror = function () {
            console.error('XHR network error: unable to complete the request');
            onError(0, 'network error');
        };
        xhr.onreadystatechange = function () {
            if (xhr.readyState != 4) {
                return;
            }
            if (xhr.status === 0) {
                return;
            }
            if (xhr.status === 200) {
                const contentType = xhr.getResponseHeader('Content-Type');
                if (contentType && contentType.includes('application/json')) {
                    try {
                        onSuccess(JSON.parse(xhr.responseText));
                    } catch (e) {
                        console.error(`XHR JSON.parse error: unable to parse JSON: ${e.message}`);
                        onError(-1, e.message);
                    }
                    return;
                }
                onSuccess(xhr.responseText);
                return;
            }
            // handle HTTP error
            console.error(`XHR HTTP error: ${xhr.status}: ${xhr.statusText}`);
            onError(xhr.status, xhr.responseText)
        };
        // debug: console.info(`POST ${url} data:`, obj);
        xhr.send(JSON.stringify(obj));
    }
}

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

    centerWithin(rect) {
        return this.centerX() >= rect.left() && this.centerX() <= rect.left() + rect.width() &&
                this.centerY() >= rect.top() && this.centerY() <= rect.top() + rect.height();
    }
}

function ajax() {
    return new AJAX();
}

function onItemMouseDown(e, item) {
    if (e.button != BUTTON_LEFT) {
        return;
    }
    let initialMouseX = e.clientX;
    let initialMouseY = e.clientY;

    let initialItemX = parseInt(item.style.left);
    let initialItemY = parseInt(item.style.top);

    function onMouseMove(event) {
        const deltaX = event.clientX - initialMouseX;
        const deltaY = event.clientY - initialMouseY;

        item.style.left = `${initialItemX + deltaX}px`;
        item.style.top = `${initialItemY + deltaY}px`;

        item.info.x = parseInt(item.style.left);
        item.info.y = parseInt(item.style.top);

        ajax().postJSON(`${window.location.pathname}/update`, item.info);
    }

    document.addEventListener('mousemove', onMouseMove);

    document.addEventListener('mouseup', () => {
        // console.info(`DEBUG item id=${item.id} mouse up:`, item.info);
        ajax().postJSON(`${window.location.pathname}/update`, item.info);

        if (item.info.class == 'chip') {
            // XXX: accountChip has to be called in exactly in this handler.
            // Otherwise the following situation will not be handled correctly:
            // - when a chip that is being dragged stops under another chip.
            // In this case the event will be called with the top most item with
            // regard to z-index.
            const slots = document.querySelectorAll('.player_slot');
            accountChip(item, slots);
            slots.forEach(updateSlotsWithMoney);
        }
        document.removeEventListener('mousemove', onMouseMove);
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
    item.addEventListener('mousedown', (e) => { onItemMouseDown(e, item); });

    item.render = () => {};

    return item;
}

function renderCard(card) {
    let text = '';
    let color = 'black';
    let side = card.info.side;
    let css = `card-${side}`

    card.classList.remove('card-cover', 'card-face', 'owned');
    card.style.borderColor = '';
    if (card.info.owner_id != '') {
        card.classList.add('owned');
        card.style.borderColor = players[card.info.owner_id].color || 'purple';
        // console.info(players[card.info.owner_id]);
    }
    if (side == FACE) {
        text = `${card.info.rank} ${card.info.suit}`;
        color = card.info.suit == '♥' || card.info.suit == '♦' ? 'red': 'black';
    }
    card.innerText = text;
    card.classList.add(css);
    card.style.color = color;
}

function takeCard(card) {
    if (card.info.owner_id != '') {
        return; // already owned
    }
    ajax().success((resp) => {
        console.info('take card result: ', resp);
        if (resp.updated != null) {
            updateItem(resp.updated);
        }
    }).postJSON(`${window.location.pathname}/take_card`,
        {'id': card.info.id});
}

function showCard(card) {
    if (card.info.owner_id != getSession().user_id) {
        return; // can't show not owned cards
    }
    ajax().success((resp) => {
        console.info('show card result: ', resp);
        if (resp.updated != null) {
            updateItem(resp.updated);
        }
    }).postJSON(`${window.location.pathname}/show_card`,
        {'id': card.info.id});
}

function onCardDblClick(e, card) {
    console.log('DEBUG onCardDblClick', e.button);
    if (e.button != BUTTON_LEFT) {
        return;
    }
    if (card.info.owner_id != '' && card.info.owner_id != getSession().user_id) {
        return; // can't turn other player cards cards
    }
    card.info.side = card.info.side == COVER ? FACE: COVER;
    ajax().success((resp) => {
        if (resp.updated == null) {
            return;
        }
        card.info = resp.updated;
        updateItem(resp.updated);
    }).postJSON(`${window.location.pathname}/update`, card.info)
}

function newCard(info, x, y) {
    const card = newItem('card', info, x, y);
    card.addEventListener('click', (e) => {
        console.log('DEBUG', isKeyTPressed, isKeyOPressed, e);
        if (e.button != BUTTON_LEFT) {
            return;
        }
        if (e.ctrlKey || isKeyTPressed || e.metaKey) {
            takeCard(card);
        }
        if (e.shiftKey || isKeyOPressed) {
            showCard(card);
        }
    });
    card.addEventListener('dblclick', (e) => { onCardDblClick(e, card) });
    card.render = () => {  renderCard(card); };
    card.render();
    return card;
}

function accountChip(chip, slots) {
    if (chip == null) {
        return;
    }
    const rect = new Rect(chip);
    // console.log(`DEBUG chip id=${chip.id} accountChip`);
    for (let slot of slots) {
        if (slot.chips == null) {
            continue;
        }
        slotRect = new Rect(slot);
        if (chip.id in slot.chips) {
            delete slot.chips[chip.id];
        }
        if (rect.centerWithin(slotRect)) {
            slot.chips[chip.id] = chip;
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
    // HACK: gets data from global state due to .color property conflict
    const player = players[info.owner_id];
    item.classList.add(player.skin);
    item.innerText = player.Name;

    const slot = document.getElementById(`slot-${player.index}`);
    slot.playerElem = item;
    slot.chips = {};

    item.render = () => {
        // HACK
        item.style.left = ''; // use from css
        item.style.top = ''; // use from css
    };
    item.render();
    return item;
}

function updateSlotsWithMoney(slot) {
    if (slot.chips == null || slot.playerElem == null) {
        return;
    }
    const vals = Object.values(slot.chips).map(chip => chip.info.val);
    const total = vals.reduce((s, x) => s + x, 0);;
    const player = players[slot.playerElem.info.owner_id];
    slot.playerElem.innerText = `${player.Name}: ${total}$`;
}

function updateItem(src) {
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
    item.render();
}

function updateTable(resp) {
    const chips = [];
    players = resp.Players;
    for (let it of resp.Items) {
        updateItem(it);
        if (it.class == 'chip') {
            chips.push(it);
        }
    }
    const slots = document.querySelectorAll('.player_slot');
    for (let it of chips) {
        const item = document.getElementById(`item-${it.id}`);
        accountChip(item, slots);
    }
    slots.forEach(updateSlotsWithMoney);
}

function refresh(items) {
    ajax().success((resp) => {
        updateTable(resp)
    }).error((status, msg) => {
        if (status === 401) {
            window.location = '/';
        }
        console.error('refersh', status, msg);
    }).get(`${window.location.pathname}/state`);
}

function createItem(info) {
    const table = document.getElementById('card-table');
    let item = null;
    switch (info.class) {
    case 'card':
        item = newCard(info, info.x, info.y);
        break;
    case 'chip':
        item = newChip(info, info.x, info.y);
        break;
    case 'dealer':
        item = newDealer(info, info.x, info.y);
        break;
    case 'player':
        item = newPlayer(info, info.x, info.y);
        break;
    default:
        throw new Exception(`unknown item class: ${info.class}`)
    }
    table.appendChild(item);
    return item;
}

function isKeyPressed(e, key) {
    try {
        return e.key.toLowerCase() === key;
    } catch {
        return false;
    }
}

function onLoad() {
    document.addEventListener('keydown', (event) => {
        if (isKeyPressed(event, 't')) {
            isKeyTPressed = true;
        }
        if (isKeyPressed(event, 'o')) {
            isKeyOPressed = true;
        }
    });
    document.addEventListener('keyup', (event) => {
        if (isKeyPressed(event, 't')) {
            isKeyTPressed = false;
        }
        if (isKeyPressed(event, 'o')) {
            isKeyOPressed = true;
        }
    });

    ajax().success((resp) => {
        console.info('initial table fetch:', resp);
        updateTable(resp);
        // setInterval(() => {
        //     refresh();
        // }, 10000);

        const socket = new WebSocket(`ws://${window.location.host}${window.location.pathname}/listen`);
        socket.onopen = () => {
            console.log('websocket connected');
            let banner = document.getElementById('offline-banner');
            banner.style.display = 'none';
        };
        socket.onclose = () => {
            console.log('websocket disconnected');
            let banner = document.getElementById('offline-banner');
            banner.style.display = 'block';
        };
        socket.onerror = (err) => { console.error('websocket error:', err); };
        socket.onmessage = (event) => {
            // console.log('websocket message:', typeof event.data);
            try {
                resp = JSON.parse(event.data)
                updateTable(resp);
            } catch (e) {
                // non-JSON payload
                if (event.data === 'refresh') {
                    location.reload();
                    return;
                }
                console.log(event.data);
            }
        };
    }).get(`${window.location.pathname}/state?cw=${window.screen.availWidth}&ch=${window.screen.availHeight}`);
}
